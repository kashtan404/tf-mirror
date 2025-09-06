package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tf-mirror/internal/common"
	"tf-mirror/internal/downloader"
	binaries "tf-mirror/internal/downloader/binaries"
	"tf-mirror/internal/server"
)

// Mode represents the application mode
type Mode string

const (
	ModeDownloader Mode = "downloader"
	ModeServer     Mode = "server"
)

func main() {
	// Common flags
	var (
		mode    = flag.String("mode", "", "Application mode: 'downloader' or 'server' (required)")
		help    = flag.Bool("help", false, "Show help message")
		version = flag.Bool("version", false, "Show version information")
		debug   = flag.Bool("debug", false, "Enable debug logging")

		// Downloader flags
		proxy            = flag.String("proxy", "", "HTTP/HTTPS/SOCKS proxy URL for downloading packages")
		checkPeriod      = flag.Int("check-period", 24, "Period for checking new versions in hours")
		downloadPath     = flag.String("download-path", "", "Directory for downloading packages (required for downloader mode)")
		providerFilter   = flag.String("provider-filter", "", "Comma-separated list of providers to download (namespace/name format, e.g., 'hashicorp/aws,hashicorp/helm')")
		platformFilter   = flag.String("platform-filter", "", "Comma-separated list of platforms to download (os_arch format, e.g., 'linux_amd64,darwin_arm64')")
		maxAttempts      = flag.Int("max-attempts", 5, "Maximum download attempts per provider (default: 5)")
		downloadTimeout  = flag.Int("download-timeout", 180, "Download timeout per attempt in seconds (default: 180)")
		downloadBinaries = flag.String("download-binaries", "", "Comma-separated list of binaries to download from releases.hashicorp.com (e.g., 'consul>1.21.3,nomad>1.6.0')")

		// Server flags
		listenHost = flag.String("listen-host", "", "Address to listen on (default: all interfaces)")
		listenPort = flag.Int("listen-port", 80, "Port to listen on")
		hostname   = flag.String("hostname", "", "DNS hostname of the server (optional)")
		enableTLS  = flag.Bool("enable-tls", false, "Enable HTTPS")
		tlsCert    = flag.String("tls-crt", "", "Path to TLS certificate file (required if --enable-tls is set)")
		tlsKey     = flag.String("tls-key", "", "Path to TLS private key file (required if --enable-tls is set)")
		dataPath   = flag.String("data-path", "", "Path to directory containing downloaded packages (required for server mode)")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Terraform Registry Mirror - Unified Application\n\n")
		fmt.Fprintf(os.Stderr, "This application can run in two modes:\n")
		fmt.Fprintf(os.Stderr, "  downloader - Downloads provider packages from registry.terraform.io\n")
		fmt.Fprintf(os.Stderr, "  server     - Serves downloaded packages as a registry mirror\n\n")
		fmt.Fprintf(os.Stderr, "Common Options:\n")
		fmt.Fprintf(os.Stderr, "  --mode string\n")
		fmt.Fprintf(os.Stderr, "    	Application mode: 'downloader' or 'server' (required)\n")
		fmt.Fprintf(os.Stderr, "  --help\n")
		fmt.Fprintf(os.Stderr, "    	Show help message\n")
		fmt.Fprintf(os.Stderr, "  --version\n")
		fmt.Fprintf(os.Stderr, "    	Show version information\n")
		fmt.Fprintf(os.Stderr, "  --debug\n")
		fmt.Fprintf(os.Stderr, "    	Enable debug logging\n")
		fmt.Fprintf(os.Stderr, "\nDownloader Mode Options:\n")
		fmt.Fprintf(os.Stderr, "  --download-path string\n")
		fmt.Fprintf(os.Stderr, "    	Directory for downloading packages (required)\n")
		fmt.Fprintf(os.Stderr, "  --proxy string\n")
		fmt.Fprintf(os.Stderr, "    	HTTP/HTTPS/SOCKS proxy URL for downloading packages\n")
		fmt.Fprintf(os.Stderr, "  --check-period int\n")
		fmt.Fprintf(os.Stderr, "    	Period for checking new versions in hours (default 24)\n")
		fmt.Fprintf(os.Stderr, "  --provider-filter string\n")
		fmt.Fprintf(os.Stderr, "    	Comma-separated list of providers (e.g., 'hashicorp/aws,hashicorp/helm')\n")
		fmt.Fprintf(os.Stderr, "  --platform-filter string\n")
		fmt.Fprintf(os.Stderr, "    	Comma-separated list of platforms (e.g., 'linux_amd64,darwin_arm64')\n")
		fmt.Fprintf(os.Stderr, "  --max-attempts int\n")
		fmt.Fprintf(os.Stderr, "    	Maximum download attempts per provider (default: 5)\n")
		fmt.Fprintf(os.Stderr, "  --download-timeout int\n")
		fmt.Fprintf(os.Stderr, "    	Download timeout per attempt in seconds (default: 180)\n")
		fmt.Fprintf(os.Stderr, "\nServer Mode Options:\n")
		fmt.Fprintf(os.Stderr, "  --data-path string\n")
		fmt.Fprintf(os.Stderr, "    	Path to directory containing downloaded packages (required)\n")
		fmt.Fprintf(os.Stderr, "  --listen-host string\n")
		fmt.Fprintf(os.Stderr, "    	Address to listen on (default: all interfaces)\n")
		fmt.Fprintf(os.Stderr, "  --listen-port int\n")
		fmt.Fprintf(os.Stderr, "    	Port to listen on (default 80)\n")
		fmt.Fprintf(os.Stderr, "  --hostname string\n")
		fmt.Fprintf(os.Stderr, "    	DNS hostname of the server (optional)\n")
		fmt.Fprintf(os.Stderr, "  --enable-tls\n")
		fmt.Fprintf(os.Stderr, "    	Enable HTTPS\n")
		fmt.Fprintf(os.Stderr, "  --tls-crt string\n")
		fmt.Fprintf(os.Stderr, "    	Path to TLS certificate file (required if --enable-tls is set)\n")
		fmt.Fprintf(os.Stderr, "  --tls-key string\n")
		fmt.Fprintf(os.Stderr, "    	Path to TLS private key file (required if --enable-tls is set)\n")
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  TF_MIRROR_MODE         Same as --mode\n")
		fmt.Fprintf(os.Stderr, "  PROXY                  Same as --proxy\n")
		fmt.Fprintf(os.Stderr, "  CHECK_PERIOD           Same as --check-period\n")
		fmt.Fprintf(os.Stderr, "  DOWNLOAD_PATH          Same as --download-path\n")
		fmt.Fprintf(os.Stderr, "  PROVIDER_FILTER        Same as --provider-filter\n")
		fmt.Fprintf(os.Stderr, "  PLATFORM_FILTER        Same as --platform-filter\n")
		fmt.Fprintf(os.Stderr, "  MAX_ATTEMPTS           Same as --max-attempts\n")
		fmt.Fprintf(os.Stderr, "  DOWNLOAD_TIMEOUT       Same as --download-timeout\n")
		fmt.Fprintf(os.Stderr, "  LISTEN_HOST            Same as --listen-host\n")
		fmt.Fprintf(os.Stderr, "  LISTEN_PORT            Same as --listen-port\n")
		fmt.Fprintf(os.Stderr, "  HOSTNAME               Same as --hostname\n")
		fmt.Fprintf(os.Stderr, "  ENABLE_TLS             Same as --enable-tls\n")
		fmt.Fprintf(os.Stderr, "  TLS_CRT                Same as --tls-crt\n")
		fmt.Fprintf(os.Stderr, "  TLS_KEY                Same as --tls-key\n")
		fmt.Fprintf(os.Stderr, "  DATA_PATH              Same as --data-path\n")
		fmt.Fprintf(os.Stderr, "  DEBUG                  Same as --debug\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Run as downloader\n")
		fmt.Fprintf(os.Stderr, "  %s --mode downloader --download-path ./data\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Run as server\n")
		fmt.Fprintf(os.Stderr, "  %s --mode server --data-path ./data --listen-port 8080\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Downloader with filters\n")
		fmt.Fprintf(os.Stderr, "  %s --mode downloader --download-path ./data \\\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    --provider-filter 'hashicorp/aws,hashicorp/helm' \\\n")
		fmt.Fprintf(os.Stderr, "    --platform-filter 'linux_amd64,darwin_arm64'\n")
	}

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *version {
		fmt.Println(common.GetFullVersionString())
		os.Exit(0)
	}

	// Override with environment variables if not set via flags
	if *mode == "" {
		*mode = common.GetEnvWithDefault("TF_MIRROR_MODE", "")
	}
	if *proxy == "" {
		*proxy = os.Getenv("PROXY")
	}
	if *downloadPath == "" {
		*downloadPath = os.Getenv("DOWNLOAD_PATH")
	}
	if *dataPath == "" {
		*dataPath = os.Getenv("DATA_PATH")
	}
	if *listenHost == "" {
		*listenHost = os.Getenv("LISTEN_HOST")
	}
	if *hostname == "" {
		*hostname = os.Getenv("HOSTNAME")
	}
	if *tlsCert == "" {
		*tlsCert = os.Getenv("TLS_CRT")
	}
	if *tlsKey == "" {
		*tlsKey = os.Getenv("TLS_KEY")
	}
	if *providerFilter == "" {
		*providerFilter = os.Getenv("PROVIDER_FILTER")
	}
	if *platformFilter == "" {
		*platformFilter = os.Getenv("PLATFORM_FILTER")
	}
	if *downloadBinaries == "" {
		*downloadBinaries = os.Getenv("DOWNLOAD_BINARIES")
	}
	if envMaxAttempts := os.Getenv("MAX_ATTEMPTS"); envMaxAttempts != "" && *maxAttempts == 5 {
		if val, err := common.ParseEnvInt("MAX_ATTEMPTS", 5); err == nil {
			*maxAttempts = val
		}
	}
	if envDownloadTimeout := os.Getenv("DOWNLOAD_TIMEOUT"); envDownloadTimeout != "" && *downloadTimeout == 180 {
		if val, err := common.ParseEnvInt("DOWNLOAD_TIMEOUT", 180); err == nil {
			*downloadTimeout = val
		}
	}

	// Parse environment variables for boolean and integer values
	if !*enableTLS {
		if enableTLSEnv, err := common.ParseEnvBool("ENABLE_TLS", false); err == nil {
			*enableTLS = enableTLSEnv
		}
	}
	if !*debug {
		if debugEnv, err := common.ParseEnvBool("DEBUG", false); err == nil {
			*debug = debugEnv
		}
	}
	if envCheckPeriod := os.Getenv("CHECK_PERIOD"); envCheckPeriod != "" && *checkPeriod == 24 {
		if period, err := common.ParseEnvInt("CHECK_PERIOD", 24); err == nil {
			*checkPeriod = period
		}
	}
	if envListenPort := os.Getenv("LISTEN_PORT"); envListenPort != "" && *listenPort == 80 {
		if port, err := common.ParseEnvInt("LISTEN_PORT", 80); err == nil {
			*listenPort = port
		}
	}

	// Validate mode
	if *mode == "" {
		fmt.Fprintf(os.Stderr, "Error: --mode is required. Use 'downloader' or 'server'\n\n")
		flag.Usage()
		os.Exit(1)
	}

	appMode := Mode(*mode)
	if appMode != ModeDownloader && appMode != ModeServer {
		fmt.Fprintf(os.Stderr, "Error: invalid mode '%s'. Use 'downloader' or 'server'\n\n", *mode)
		flag.Usage()
		os.Exit(1)
	}

	// Create logger
	logger := common.NewLogger()
	if *debug {
		os.Setenv("DEBUG", "1")
	}

	logger.Info("Starting Terraform Registry Mirror")
	logger.Info("Version: %s", common.GetVersionString())
	logger.Info("Mode: %s", appMode)

	// Run appropriate mode
	switch appMode {
	case ModeDownloader:
		runDownloader(logger, *downloadPath, *proxy, *checkPeriod, *providerFilter, *platformFilter, *maxAttempts, *downloadTimeout, *downloadBinaries)
	case ModeServer:
		runServer(logger, *dataPath, *listenHost, *listenPort, *hostname, *enableTLS, *tlsCert, *tlsKey)
	}
}

func runDownloader(logger *common.Logger, downloadPath, proxy string, checkPeriod int, providerFilter, platformFilter string, maxAttempts int, downloadTimeout int, downloadBinaries string) {
	// Validate required parameters for downloader
	if downloadPath == "" {
		logger.Fatal("Error: --download-path is required for downloader mode")
	}

	if checkPeriod <= 0 {
		logger.Fatal("Error: --check-period must be positive")
	}

	// Create download directory if it doesn't exist
	if err := os.MkdirAll(downloadPath, 0755); err != nil {
		logger.Fatal("Failed to create download directory: %v", err)
	}

	logger.Info("Downloader Configuration:")
	logger.Info("  Download path: %s", downloadPath)
	logger.Info("  Check period: %d hours", checkPeriod)
	if proxy != "" {
		logger.Info("  Proxy: %s", proxy)
	} else {
		logger.Info("  Proxy: none")
	}
	if providerFilter != "" {
		logger.Info("  Provider filter: %s", providerFilter)
	} else {
		logger.Info("  Provider filter: all providers")
	}
	if platformFilter != "" {
		logger.Info("  Platform filter: %s", platformFilter)
	} else {
		logger.Info("  Platform filter: all supported platforms")
	}

	// Create downloader configuration
	downloaderConfig := &common.DownloaderConfig{
		ProxyURL:         proxy,
		CheckPeriod:      time.Duration(checkPeriod) * time.Hour,
		DownloadPath:     downloadPath,
		MaxConcurrent:    common.DefaultMaxConcurrent,
		ProviderFilter:   providerFilter,
		PlatformFilter:   platformFilter,
		MaxAttempts:      maxAttempts,
		DownloadTimeout:  time.Duration(downloadTimeout) * time.Second,
		DownloadBinaries: downloadBinaries,
	}

	// Create registry configuration
	registryConfig := &common.RegistryConfig{
		BaseURL:    common.TerraformRegistryURL,
		ProxyURL:   proxy,
		UserAgent:  common.UserAgent,
		Timeout:    common.DefaultTimeout,
		MaxRetries: common.DefaultMaxRetries,
	}

	// Create and start downloader service
	service, err := downloader.NewService(downloaderConfig, registryConfig, logger)
	if err != nil {
		logger.Fatal("Failed to create downloader service: %v", err)
	}
	defer service.Close()

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal: %s", sig)
		cancel()
	}()

	// Start the service
	if err := service.StartWithContext(ctx); err != nil {
		logger.Fatal("Downloader service failed: %v", err)
	}

	// После скачивания провайдеров и генерации индексов — скачиваем бинарники HashiCorp, если требуется
	if downloadBinaries != "" {
		logger.Info("Starting download of HashiCorp binaries from releases.hashicorp.com")
		binFilters, err := binaries.ParseBinaryFilter(downloadBinaries)
		if err != nil {
			logger.Error("Failed to parse download-binaries filter: %v", err)
			return
		}
		platforms := binaries.SupportedPlatforms()
		_, err = binaries.DownloadHashiCorpBinaries(downloadPath, binFilters, platforms, func(format string, args ...interface{}) {
			logger.Info(format, args...)
		})
		if err != nil {
			logger.Error("Failed to download HashiCorp binaries: %v", err)
		} else {
			logger.Info("HashiCorp binaries download completed")
		}
	}
}

func runServer(logger *common.Logger, dataPath, listenHost string, listenPort int, hostname string, enableTLS bool, tlsCert, tlsKey string) {
	// Validate required parameters for server
	if dataPath == "" {
		logger.Fatal("Error: --data-path is required for server mode")
	}

	if enableTLS {
		if tlsCert == "" || tlsKey == "" {
			logger.Fatal("Error: --tls-crt and --tls-key are required when --enable-tls is set")
		}

		// Verify TLS files exist
		if _, err := os.Stat(tlsCert); os.IsNotExist(err) {
			logger.Fatal("Error: TLS certificate file does not exist: %s", tlsCert)
		}
		if _, err := os.Stat(tlsKey); os.IsNotExist(err) {
			logger.Fatal("Error: TLS key file does not exist: %s", tlsKey)
		}
	}

	// Verify data path exists
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		logger.Fatal("Error: Data path does not exist: %s", dataPath)
	}

	if listenPort <= 0 || listenPort > 65535 {
		logger.Fatal("Error: --listen-port must be between 1 and 65535")
	}

	logger.Info("Server Configuration:")
	logger.Info("  Listen address: %s:%d", listenHost, listenPort)
	logger.Info("  Data path: %s", dataPath)
	if hostname != "" {
		logger.Info("  Hostname: %s", hostname)
	}
	if enableTLS {
		logger.Info("  TLS enabled: yes")
		logger.Info("  Certificate: %s", tlsCert)
		logger.Info("  Private key: %s", tlsKey)
	} else {
		logger.Info("  TLS enabled: no")
	}

	// Create server configuration
	config := &common.ServerConfig{
		ListenHost: listenHost,
		ListenPort: listenPort,
		Hostname:   hostname,
		EnableTLS:  enableTLS,
		TLSCert:    tlsCert,
		TLSKey:     tlsKey,
		DataPath:   dataPath,
	}

	// Create server
	srv := server.NewServer(config, logger)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal: %s", sig)
		cancel()
	}()

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		logger.Info("Shutdown signal received, stopping server...")

		// Create shutdown context with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := srv.Stop(shutdownCtx); err != nil {
			logger.Error("Error during server shutdown: %v", err)
			os.Exit(1)
		}
		logger.Info("Server stopped gracefully")

	case err := <-serverErr:
		logger.Fatal("Server failed to start: %v", err)
	}
}
