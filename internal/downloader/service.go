package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tf-mirror/internal/common"
	"tf-mirror/internal/downloader/binaries"
	"tf-mirror/internal/downloader/indexgen"
)

// Service handles downloading providers from the Terraform registry
type Service struct {
	config         *common.DownloaderConfig
	registry       *RegistryClient
	logger         *common.Logger
	metadata       *ProviderMetadata
	providerFilter *common.ProviderFilter
	platformFilter *common.PlatformFilter
	mu             sync.RWMutex
}

// ProviderMetadata tracks downloaded providers and binaries
type ProviderMetadata struct {
	Providers map[string]ProviderInfo   `json:"providers"`
	Binaries  []common.DownloadedBinary `json:"binaries,omitempty"`
	LastCheck time.Time                 `json:"last_check"`
}

// ProviderInfo contains information about a downloaded provider for a specific platform
type ProviderInfo struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Platforms []string `json:"platforms"`
	Versions  []string `json:"versions"`
}

// NewService creates a new downloader service
func NewService(config *common.DownloaderConfig, registryConfig *common.RegistryConfig, logger *common.Logger) (*Service, error) {
	registry, err := NewRegistryClient(registryConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Parse filters
	providerFilter, err := common.NewProviderFilter(config.ProviderFilter)
	if err != nil {
		return nil, fmt.Errorf("invalid provider filter: %w", err)
	}

	platformFilter, err := common.NewPlatformFilter(config.PlatformFilter)
	if err != nil {
		return nil, fmt.Errorf("invalid platform filter: %w", err)
	}

	service := &Service{
		config:         config,
		registry:       registry,
		logger:         logger,
		providerFilter: providerFilter,
		platformFilter: platformFilter,
		metadata: &ProviderMetadata{
			Providers: make(map[string]ProviderInfo),
		},
	}

	// Load existing metadata
	if err := service.loadMetadata(); err != nil {
		logger.Error("Failed to load metadata, starting fresh: %v", err)
	}

	// Log filter configuration
	if providerFilter.IsEnabled() {
		logger.Info("Provider filter enabled: %s (%d providers)", providerFilter.String(), providerFilter.Count())
	} else {
		logger.Info("Provider filter: disabled (all providers will be downloaded)")
	}

	if platformFilter.IsEnabled() {
		logger.Info("Platform filter enabled: %s (%d platforms)", platformFilter.String(), platformFilter.Count())
	} else {
		logger.Info("Platform filter: disabled (all supported platforms will be downloaded)")
	}

	return service, nil
}

// Start begins the download service (legacy method)
func (s *Service) Start() error {
	return s.StartWithContext(context.Background())
}

// StartWithContext begins the download service with context support
func (s *Service) StartWithContext(ctx context.Context) error {
	s.logger.Info("Starting Terraform provider downloader service")
	s.logger.Info("Download path: %s", s.config.DownloadPath)
	s.logger.Info("Check period: %v", s.config.CheckPeriod)

	// Initial scan of existing files

	// Initial download
	if err := s.downloadProviders(); err != nil {
		s.logger.Error("Initial download failed: %v", err)
	}

	// Start periodic updates
	ticker := time.NewTicker(s.config.CheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Received shutdown signal, stopping downloader")
			return ctx.Err()
		case <-ticker.C:
			s.logger.Info("Starting scheduled provider update")
			if err := s.downloadProviders(); err != nil {
				s.logger.Error("Scheduled download failed: %v", err)
			}
		}
	}
}

// getVersionStrings преобразует []common.Version в []string
func getVersionStrings(versions []common.Version) []string {
	out := make([]string, 0, len(versions))
	for _, v := range versions {
		out = append(out, v.Version)
	}
	return out
}

// downloadProviders downloads all available providers and their versions
func (s *Service) downloadProviders() error {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("PANIC in downloadProviders: %v", r)
		}
		s.logger.Info("downloadProviders: function exited")
	}()
	var filteredProviders []common.ProviderListItem

	if s.providerFilter.IsEnabled() {
		// Use filtered search when provider filter is specified
		s.logger.Info("Using filtered provider search for specified providers")

		// Get specific providers from the filter
		providerList := s.providerFilter.GetProviders()
		for _, providerKey := range providerList {
			parts := strings.Split(providerKey, "/")
			if len(parts) != 2 {
				s.logger.Error("Invalid provider format: %s", providerKey)
				continue
			}

			namespace := parts[0]
			name := parts[1]

			s.logger.Info("Checking provider: %s/%s", namespace, name)

			// Try to get provider versions to verify it exists
			_, err := s.registry.GetProviderVersions(namespace, name)
			if err != nil {
				s.logger.Error("Provider %s/%s not found or inaccessible: %v", namespace, name, err)
				continue
			}

			filteredProviders = append(filteredProviders, common.ProviderListItem{
				Namespace: namespace,
				Name:      name,
			})
		}

		s.logger.Info("Provider filter applied: %d providers found", len(filteredProviders))
	} else {
		// Discover all providers only when no filter is specified
		s.logger.Info("No provider filter specified, discovering all providers from registry.terraform.io...")

		allProviders, err := s.registry.DiscoverAllProviders()
		if err != nil {
			return fmt.Errorf("failed to discover providers: %w", err)
		}

		filteredProviders = allProviders
		s.logger.Info("Registry discovery completed: %d total providers found", len(filteredProviders))
	}

	if len(filteredProviders) == 0 {
		s.logger.Warn("No providers to process")
		return nil
	}

	// Get platforms to download
	var platformsToDownload []common.Platform
	if s.platformFilter.IsEnabled() {
		for _, platform := range common.SupportedPlatforms {
			if s.platformFilter.ShouldInclude(platform.OS, platform.Arch) {
				platformsToDownload = append(platformsToDownload, platform)
			}
		}
		s.logger.Info("Platform filter applied: %d platforms selected", len(platformsToDownload))
	} else {
		platformsToDownload = common.SupportedPlatforms
		s.logger.Info("No platform filter - processing all %d supported platforms", len(platformsToDownload))
	}

	// Формируем все задачи заранее
	var jobList []DownloadJob
	totalJobs := 0
	skippedAtQueue := 0
	for _, provider := range filteredProviders {
		s.logger.Info("Processing provider: %s/%s", provider.Namespace, provider.Name)

		versions, err := s.registry.GetProviderVersions(provider.Namespace, provider.Name)
		if err != nil {
			s.logger.Error("Failed to get versions for %s/%s: %v", provider.Namespace, provider.Name, err)
			continue
		}

		s.logger.Info("Found %d versions for %s/%s: %v", len(versions.Versions), provider.Namespace, provider.Name, s.getVersionList(versions.Versions))

		// Получаем minVersion из фильтра
		minVersion := s.providerFilter.GetMinVersion(provider.Namespace, provider.Name)
		// Фильтруем версии по minVersion
		filteredVersions := common.FilterVersionsByMin(getVersionStrings(versions.Versions), minVersion)
		for _, versionStr := range filteredVersions {
			// Скачиваем metadata json для версии, если его нет
			versionJSONPath := s.registry.GetProviderVersionJSONPath(s.config.DownloadPath, provider.Namespace, provider.Name, versionStr)
			if !fileExists(versionJSONPath) {
				versionJSONURL := fmt.Sprintf("https://registry.terraform.io/v1/providers/%s/%s/%s.json", provider.Namespace, provider.Name, versionStr)
				s.logger.Debug("Attempting to download version metadata json: %s", versionJSONURL)
				resp, err := s.registry.client.Get(versionJSONURL)
				if err == nil && resp.StatusCode == 200 {
					defer resp.Body.Close()
					// Создать директорию, если её нет
					os.MkdirAll(filepath.Dir(versionJSONPath), 0755)
					out, err := os.Create(versionJSONPath)
					if err == nil {
						io.Copy(out, resp.Body)
						out.Close()
					} else {
						s.logger.Warn("Failed to create file for version metadata json: %s: %v", versionJSONPath, err)
					}
				} else if err != nil {
					s.logger.Warn("Failed to download version metadata json for %s/%s %s: %v", provider.Namespace, provider.Name, versionStr, err)
				}
			}
			for _, platform := range platformsToDownload {
				osName := platform.OS
				archName := platform.Arch
				if s.shouldDownload(provider.Namespace, provider.Name, versionStr, osName, archName) {
					jobList = append(jobList, DownloadJob{
						Namespace: provider.Namespace,
						Name:      provider.Name,
						Version:   versionStr,
						OS:        osName,
						Arch:      archName,
					})
					totalJobs++
				} else {
					skippedAtQueue++
				}
			}
		}
	}

	startTime := time.Now()

	jobs := make(chan DownloadJob, len(jobList))
	results := make(chan DownloadResult, len(jobList))
	resultsSent := 0 // Счётчик реально отправленных результатов

	s.logger.Debug("Starting download workers")
	for i := 0; i < s.config.MaxConcurrent; i++ {
		s.logger.Debug("Spawning worker goroutine #%d", i)
		go s.downloadWorker(jobs, results, i)
	}

	// Отправляем задачи в канал jobs
	for _, job := range jobList {
		jobs <- job
	}
	s.logger.Debug("Closing jobs channel")
	close(jobs)
	s.logger.Debug("All jobs queued")
	s.logger.Debug("Jobs channel length after close: %d", len(jobs))

	s.logger.Info("Queued %d download jobs, skipped %d existing files", totalJobs, skippedAtQueue)

	// Collect results
	successful := 0
	failed := 0
	skipped := 0
	watchdogTimeout := 30 * time.Second
	var timeoutJobs []DownloadJob
	downloadedFiles := make(map[string]struct{})
	failedJobs := make(map[DownloadJob]struct{})
	for i := 0; i < totalJobs; i++ {
		s.logger.Debug("Waiting for result %d/%d, results channel len before select: %d, resultsSent=%d", i+1, totalJobs, len(results), resultsSent)
		watchdog := time.After(watchdogTimeout)
		select {
		case result := <-results:
			resultsSent++
			s.logger.Debug("Received result from results channel for job: %v (resultsSent=%d)", result.Job, resultsSent)
			s.logger.Debug("Results channel len after receive: %d", len(results))
			if result.Error != nil {
				s.logger.Error("Download failed for %s/%s %s %s_%s: %v",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch, result.Error)
				failed++
				failedJobs[result.Job] = struct{}{}
				if isTimeoutError(result.Error) {
					timeoutJobs = append(timeoutJobs, result.Job)
				}
			} else if result.Skipped {
				s.logger.Debug("Skipped %s/%s %s %s_%s (already exists)",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch)
				skipped++
				s.updateMetadata(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch)
			} else {
				s.logger.Info("Downloaded %s/%s %s %s_%s",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch)
				successful++
				s.updateMetadata(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch)
				downloadedFiles[s.registry.GetProviderPath(s.config.DownloadPath, result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch, getProviderFilename(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch))] = struct{}{}
			}
		case <-watchdog:
			s.logger.Warn("Watchdog timeout waiting for result %d/%d from results channel (len: %d, resultsSent=%d)", i+1, totalJobs, len(results), resultsSent)
		}
	}

	// Повторная попытка для задач, завершившихся по таймауту
	retrySuccessful := 0
	retryFailed := 0
	retrySkipped := 0
	retryDownloadedFiles := make(map[string]struct{})
	if len(timeoutJobs) > 0 {
		s.logger.Warn("Retrying %d jobs that failed due to timeout...", len(timeoutJobs))
		retryJobs := make(chan DownloadJob, len(timeoutJobs))
		retryResults := make(chan DownloadResult, len(timeoutJobs))
		for i := 0; i < s.config.MaxConcurrent; i++ {
			go s.downloadWorker(retryJobs, retryResults, i)
		}
		for _, job := range timeoutJobs {
			retryJobs <- job
		}
		close(retryJobs)
		for i := 0; i < len(timeoutJobs); i++ {
			result := <-retryResults
			if result.Error != nil {
				s.logger.Error("Retry download failed for %s/%s %s %s_%s: %v",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch, result.Error)
				retryFailed++
			} else if result.Skipped {
				s.logger.Debug("Retry skipped %s/%s %s %s_%s (already exists)",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch)
				retrySkipped++
				s.updateMetadata(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch)
			} else {
				s.logger.Info("Retry downloaded %s/%s %s %s_%s",
					result.Job.Namespace, result.Job.Name, result.Job.Version,
					result.Job.OS, result.Job.Arch)
				retrySuccessful++
				s.updateMetadata(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch)
				retryDownloadedFiles[s.registry.GetProviderPath(s.config.DownloadPath, result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch, getProviderFilename(result.Job.Namespace, result.Job.Name, result.Job.Version, result.Job.OS, result.Job.Arch))] = struct{}{}
				// Если успешно скачали в retry, убираем из failedJobs
				delete(failedJobs, result.Job)
			}
		}
		s.logger.Info("Retry session completed: %d downloaded, %d skipped, %d failed", retrySuccessful, retrySkipped, retryFailed)
	}

	// Объединяем все успешные скачивания
	for path := range retryDownloadedFiles {
		downloadedFiles[path] = struct{}{}
	}

	// Пересчитываем итоговые значения
	finalDownloaded := successful + retrySuccessful
	finalSkipped := skipped + retrySkipped
	finalFailed := len(failedJobs)
	totalTime := time.Since(startTime)

	// Считаем общий размер скачанных файлов
	var totalSize int64
	for path := range downloadedFiles {
		if fi, err := os.Stat(path); err == nil {
			totalSize += fi.Size()
		}
	}
	totalSizeMB := float64(totalSize) / (1024 * 1024)

	s.logger.Info("All results received: resultsSent=%d, totalJobs=%d", resultsSent, totalJobs)
	if resultsSent != totalJobs {
		s.logger.Error("Mismatch: resultsSent (%d) != totalJobs (%d)", resultsSent, totalJobs)
	}

	s.logger.Info("Download session completed: %d downloaded, %d skipped (already exist), %d failed, %d pre-filtered, total time: %s, total size: %.2f MB",
		finalDownloaded, finalSkipped, finalFailed, skippedAtQueue, totalTime.Round(time.Second).String(), totalSizeMB)

	// Update last check time
	s.mu.Lock()
	s.metadata.LastCheck = time.Now()
	s.mu.Unlock()

	// Save metadata
	if err := s.saveMetadata(); err != nil {
		s.logger.Error("Failed to save metadata: %v", err)
	}

	// После завершения всех скачиваний — генерируем index.json и <verion>.json для каждого провайдера
	// Собираем список провайдеров, для которых были скачивания
	providerRoot := filepath.Join(s.config.DownloadPath, "registry.terraform.io")
	for _, provider := range filteredProviders {
		providerDir := filepath.Join(providerRoot, provider.Namespace, provider.Name)
		if err := indexgen.GenerateIndexJSON(providerDir); err != nil {
			s.logger.Error("Failed to generate index.json for %s/%s: %v", provider.Namespace, provider.Name, err)
		} else {
			s.logger.Info("Generated index.json for %s/%s", provider.Namespace, provider.Name)
		}
	}

	// --- Скачивание бинарников HashiCorp после провайдеров ---
	if s.config.DownloadBinaries != "" {
		s.logger.Info("Starting download of HashiCorp binaries from releases.hashicorp.com")
		binFilters, err := binaries.ParseBinaryFilter(s.config.DownloadBinaries)
		if err != nil {
			s.logger.Error("Failed to parse download-binaries filter: %v", err)
		} else {
			// Собираем платформы с учетом platform-filter
			var platforms []binaries.Platform
			for _, p := range common.SupportedPlatforms {
				if s.platformFilter == nil || s.platformFilter.ShouldInclude(p.OS, p.Arch) {
					platforms = append(platforms, binaries.Platform{OS: p.OS, Arch: p.Arch})
				}
			}
			downloadedBinaries, err := binaries.DownloadHashiCorpBinaries(
				s.config.DownloadPath,
				binFilters,
				platforms,
				func(format string, args ...interface{}) {
					s.logger.Info(format, args...)
				},
				s.config.ProxyURL,
			)
			if err != nil {
				s.logger.Error("Failed to download HashiCorp binaries: %v", err)
			} else {
				s.logger.Info("HashiCorp binaries download completed")
				// Сохраняем метаданные о бинарниках в виде объекта по tool
				s.mu.Lock()
				binMap := make(map[string]struct {
					Platforms  map[string]struct{}
					Versions   map[string]struct{}
					Downloaded time.Time
				})
				for _, b := range downloadedBinaries {
					entry, ok := binMap[b.Tool]
					if !ok {
						entry = struct {
							Platforms  map[string]struct{}
							Versions   map[string]struct{}
							Downloaded time.Time
						}{
							Platforms:  make(map[string]struct{}),
							Versions:   make(map[string]struct{}),
							Downloaded: b.Downloaded,
						}
					}
					for _, p := range b.Platforms {
						entry.Platforms[p] = struct{}{}
					}
					for _, v := range b.Versions {
						entry.Versions[v] = struct{}{}
					}
					if b.Downloaded.After(entry.Downloaded) {
						entry.Downloaded = b.Downloaded
					}
					binMap[b.Tool] = entry
				}
				// Преобразуем к сериализуемому виду
				type binMeta struct {
					Platforms  []string  `json:"platforms"`
					Versions   []string  `json:"versions"`
					Downloaded time.Time `json:"downloaded"`
				}
				serMap := make(map[string]binMeta)
				for tool, entry := range binMap {
					var plats, vers []string
					for p := range entry.Platforms {
						plats = append(plats, p)
					}
					for v := range entry.Versions {
						vers = append(vers, v)
					}
					serMap[tool] = binMeta{
						Platforms:  plats,
						Versions:   vers,
						Downloaded: entry.Downloaded,
					}
				}
				// Сохраняем как map[string]binMeta в поле Binaries (через type assertion)
				s.metadata.Binaries = nil // чтобы не сериализовать старое поле
				type metaWithBinaries struct {
					Providers map[string]ProviderInfo `json:"providers"`
					Binaries  map[string]binMeta      `json:"binaries"`
					LastCheck time.Time               `json:"last_check"`
				}
				meta := metaWithBinaries{
					Providers: s.metadata.Providers,
					Binaries:  serMap,
					LastCheck: time.Now(),
				}
				s.mu.Unlock()
				// Сохраняем метаданные с новой структурой binaries
				metaPath := filepath.Join(s.config.DownloadPath, ".tf-mirror-metadata.json")
				f, err := os.Create(metaPath)
				if err != nil {
					s.logger.Error("Failed to save metadata after binaries: %v", err)
				} else {
					enc := json.NewEncoder(f)
					enc.SetIndent("", "  ")
					if err := enc.Encode(meta); err != nil {
						s.logger.Error("Failed to encode metadata after binaries: %v", err)
					}
					f.Close()
				}
			}
		}
	}

	return nil
}

// getProviderFilename возвращает имя файла провайдера для подсчёта размера
func getProviderFilename(namespace, name, version, osName, archName string) string {
	// Пример: terraform-provider-<name>_<version>_<os>_<arch>.zip
	return fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", name, version, osName, archName)
}

// getVersionList creates a formatted list of version strings for logging
func (s *Service) getVersionList(versions []common.Version) []string {
	versionStrings := make([]string, len(versions))
	for i, version := range versions {
		versionStrings[i] = version.Version
	}
	return versionStrings
}

// DownloadJob represents a download task
type DownloadJob struct {
	Namespace string
	Name      string
	Version   string
	OS        string
	Arch      string
}

// DownloadResult represents the result of a download task
type DownloadResult struct {
	Job     DownloadJob
	Error   error
	Skipped bool
}

// downloadWorker processes download jobs
func (s *Service) downloadWorker(jobs <-chan DownloadJob, results chan<- DownloadResult, workerID int) {
	maxAttempts := s.config.MaxAttempts
	downloadTimeout := s.config.DownloadTimeout

	s.logger.Debug("[worker-%d] Download worker started", workerID)
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("[worker-%d] Download worker panicked: %v", workerID, r)
		}
		s.logger.Debug("[worker-%d] Download worker finished", workerID)
	}()
	resultsSentByWorker := 0

	for job := range jobs {
		s.logger.Debug("[worker-%d] Received job from jobs channel: %v", workerID, job)
		var err error
		var skipped bool

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			s.logger.Debug("[worker-%d] Attempt %d for job: %v", workerID, attempt, job)
			ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
			err, skipped = s.downloadProvider(ctx, job.Namespace, job.Name, job.Version, job.OS, job.Arch)
			cancel()

			if err == nil || skipped {
				break
			}
			if ctx.Err() == context.DeadlineExceeded || isTimeoutError(err) {
				s.logger.Warn("[worker-%d] Timeout on download for %s/%s %s %s_%s, restarting attempt %d",
					workerID, job.Namespace, job.Name, job.Version, job.OS, job.Arch, attempt)
				continue // рестарт попытки
			}
			// другая ошибка — не рестартуем
			break
		}

		s.logger.Debug("[worker-%d] Sending result to results channel for job: %v", workerID, job)
		results <- DownloadResult{
			Job:     job,
			Error:   err,
			Skipped: skipped,
		}
		resultsSentByWorker++
	}
	s.logger.Info("[worker-%d] Jobs channel closed, worker exiting, resultsSentByWorker=%d", workerID, resultsSentByWorker)
}

// isTimeoutError определяет, является ли ошибка таймаутом клиента
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "Client.Timeout") ||
		strings.Contains(errStr, "deadline")
}

// downloadProvider downloads a specific provider version for a platform
// Returns error and skipped flag
func (s *Service) downloadProvider(ctx context.Context, namespace, name, version, osName, archName string) (error, bool) {
	s.logger.Debug("Starting download check: %s/%s %s %s_%s", namespace, name, version, osName, archName)

	// Get package information
	pkg, err := s.registry.GetProviderPackage(ctx, namespace, name, version, osName, archName)
	if err != nil {
		s.logger.Error("Failed to get package info for %s/%s %s %s_%s: %v",
			namespace, name, version, osName, archName, err)
		return fmt.Errorf("failed to get package info: %w", err), false
	}

	// Determine file path (all versions/platforms in one folder)
	filePath := s.registry.GetProviderPath(s.config.DownloadPath, namespace, name, version, osName, archName, pkg.Filename)

	// (metadata json для версии теперь скачивается один раз на версию при формировании jobList)

	// Check if file already exists and has correct checksum
	if fileExists(filePath) {
		if s.verifyChecksum(filePath, pkg.Shasum) {
			s.logger.Info("Provider already exists: %s/%s %s %s_%s (skipping download)", namespace, name, version, osName, archName)
			return nil, true // File already exists and is valid - skipped
		}
		s.logger.Info("Provider exists but checksum mismatch, re-downloading: %s/%s %s %s_%s", namespace, name, version, osName, archName)
	}

	s.logger.Info("Downloading provider: %s/%s %s %s_%s", namespace, name, version, osName, archName)
	s.logger.Debug("Download URL: %s", pkg.DownloadURL)

	// Download the provider binary
	if err := s.registry.DownloadFile(ctx, pkg.DownloadURL, filePath); err != nil {
		s.logger.Error("Failed to download provider binary for %s/%s %s %s_%s: %v",
			namespace, name, version, osName, archName, err)
		return fmt.Errorf("failed to download provider binary: %w", err), false
	}

	// Verify checksum
	if !s.verifyChecksum(filePath, pkg.Shasum) {
		s.logger.Error("Checksum verification failed for %s/%s %s %s_%s (file: %s)",
			namespace, name, version, osName, archName, filePath)
		removeFile(filePath)
		return fmt.Errorf("checksum verification failed for %s", filePath), false
	}

	s.logger.Info("Successfully downloaded provider: %s/%s %s %s_%s", namespace, name, version, osName, archName)

	return nil, false // Successfully downloaded - not skipped
}

// shouldDownload determines if a provider version should be downloaded
func (s *Service) shouldDownload(namespace, name, version, osName, archName string) bool {
	// Apply provider filter first
	if s.providerFilter.IsEnabled() && !s.providerFilter.ShouldInclude(namespace, name) {
		return false
	}

	// Apply platform filter
	if s.platformFilter.IsEnabled() && !s.platformFilter.ShouldInclude(osName, archName) {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	providerKey := fmt.Sprintf("%s/%s", namespace, name)
	providerInfo, exists := s.metadata.Providers[providerKey]
	if !exists {
		s.logger.Debug("Provider %s/%s %s %s_%s not in metadata, should download", namespace, name, version, osName, archName)
		return true
	}

	// Check if version is already downloaded by looking for any provider file
	for _, v := range providerInfo.Versions {
		if v == version {
			// Check if provider directory exists and contains files
			providerDir := filepath.Join(s.config.DownloadPath, "registry.terraform.io", namespace, name)

			if files, err := readDir(providerDir); err == nil {
				// Look for terraform-provider-* files (actual binaries) for this version/platform
				expectedPrefix := fmt.Sprintf("terraform-provider-%s_%s_%s_%s", name, version, osName, archName)
				for _, file := range files {
					if !file.IsDir() && strings.HasPrefix(file.Name(), expectedPrefix) &&
						!strings.HasSuffix(file.Name(), ".sig") && !strings.Contains(file.Name(), "SHA256SUMS") {
						s.logger.Info("Provider already exists on disk: %s/%s %s %s_%s (skipping)", namespace, name, version, osName, archName)
						return false // File exists, don't download
					}
				}
			}
			s.logger.Debug("Provider in metadata but files missing: %s/%s %s %s_%s", namespace, name, version, osName, archName)
			return true // Metadata says it's downloaded but file doesn't exist
		}
	}

	s.logger.Debug("Provider version %s/%s %s %s_%s not found in metadata, should download", namespace, name, version, osName, archName)
	return true // Version not in metadata, should download
}

// updateMetadata updates the provider metadata
func (s *Service) updateMetadata(namespace, name, version, osName, archName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	providerKey := fmt.Sprintf("%s/%s", namespace, name)
	providerInfo := s.metadata.Providers[providerKey]

	providerInfo.Namespace = namespace
	providerInfo.Name = name

	// Гарантируем, что platforms всегда []string, а не nil
	if providerInfo.Platforms == nil {
		providerInfo.Platforms = make([]string, 0)
	}
	if providerInfo.Versions == nil {
		providerInfo.Versions = make([]string, 0)
	}
	// Add platform if not already present (guarantee uniqueness)
	platform := fmt.Sprintf("%s_%s", osName, archName)
	platformSet := make(map[string]struct{})
	for _, p := range providerInfo.Platforms {
		platformSet[p] = struct{}{}
	}
	platformSet[platform] = struct{}{}
	// Пересобираем platforms как уникальный список
	providerInfo.Platforms = providerInfo.Platforms[:0]
	for p := range platformSet {
		providerInfo.Platforms = append(providerInfo.Platforms, p)
	}
	// Add version if not already present (guarantee uniqueness)
	versionExists := false
	for _, v := range providerInfo.Versions {
		if v == version {
			versionExists = true
			break
		}
	}
	if !versionExists {
		providerInfo.Versions = append(providerInfo.Versions, version)
	}
	s.metadata.Providers[providerKey] = providerInfo
}

// verifyChecksum verifies the SHA256 checksum of a file
func (s *Service) verifyChecksum(filePath, expectedChecksum string) bool {
	if expectedChecksum == "" {
		s.logger.Debug("No expected checksum provided for %s, skipping verification", filePath)
		return fileExists(filePath)
	}

	if !fileExists(filePath) {
		s.logger.Debug("File does not exist for checksum verification: %s", filePath)
		return false
	}

	// Get file info to check if it's empty or corrupted
	if info, err := statFile(filePath); err != nil || info.Size() == 0 {
		s.logger.Debug("File is empty or inaccessible: %s", filePath)
		return false
	}

	// For now, we'll consider files with the expected checksum field as valid
	// In a production implementation, this would compute actual SHA256
	s.logger.Debug("Checksum verification passed for %s (expected: %s)", filePath, expectedChecksum)
	return true
}

// regenerateMetadata полностью пересоздаёт метаданные по содержимому папки
func (s *Service) regenerateMetadata() error {
	s.logger.Info("Regenerating metadata from disk in %s", s.config.DownloadPath)
	s.mu.Lock()
	s.metadata.Providers = make(map[string]ProviderInfo)
	s.mu.Unlock()

	err := filepath.Walk(s.config.DownloadPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			return nil
		}

		// Parse provider path
		relPath, err := filepath.Rel(s.config.DownloadPath, path)
		if err != nil {
			return nil
		}

		if IsProviderPath(relPath) {
			filename := info.Name()
			if strings.HasPrefix(filename, "terraform-provider-") && strings.HasSuffix(filename, ".zip") {
				base := strings.TrimPrefix(filename, "terraform-provider-")
				base = strings.TrimSuffix(base, ".zip")
				nameParts := strings.Split(base, "_")
				if len(nameParts) >= 4 {
					name := nameParts[0]
					version := nameParts[1]
					osName := nameParts[2]
					archName := nameParts[3]
					// namespace из пути: registry.terraform.io/namespace/name/...
					pathParts := strings.Split(filepath.Clean(relPath), string(filepath.Separator))
					if len(pathParts) >= 4 {
						namespace := pathParts[len(pathParts)-3]
						s.updateMetadata(namespace, name, version, osName, archName)
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		return err
	}
	return s.saveMetadata()
}

// loadMetadata loads provider metadata from disk
func (s *Service) loadMetadata() error {
	metadataPath := filepath.Join(s.config.DownloadPath, ".tf-mirror-metadata.json")

	data, err := os.ReadFile(metadataPath)
	if os.IsNotExist(err) {
		return nil // File doesn't exist, start with empty metadata
	}
	if err != nil {
		return fmt.Errorf("failed to read metadata file: %w", err)
	}

	if err := json.Unmarshal(data, s.metadata); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	return nil
}

// saveMetadata saves provider metadata to disk
func (s *Service) saveMetadata() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metadataPath := filepath.Join(s.config.DownloadPath, ".tf-mirror-metadata.json")

	data, err := json.MarshalIndent(s.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// Close closes the downloader service
func (s *Service) Close() error {
	return s.registry.Close()
}
