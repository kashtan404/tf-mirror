package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"tf-mirror/internal/common"
)

// RegistryClient handles communication with the Terraform registry
type RegistryClient struct {
	client  *common.HTTPClient
	baseURL string
	logger  *common.Logger
}

// NewRegistryClient creates a new registry client
func NewRegistryClient(config *common.RegistryConfig, logger *common.Logger) (*RegistryClient, error) {
	client, err := common.NewHTTPClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &RegistryClient{
		client:  client,
		baseURL: config.BaseURL,
		logger:  logger,
	}, nil
}

// DiscoverAllProviders discovers all available providers from the registry
func (r *RegistryClient) DiscoverAllProviders() ([]common.ProviderListItem, error) {
	r.logger.Info("Discovering all providers from registry.terraform.io...")

	var allProviders []common.ProviderListItem
	offset := 0
	limit := 100 // Registry pagination limit

	for {
		r.logger.Debug("Fetching providers with offset=%d, limit=%d", offset, limit)

		url := fmt.Sprintf("%s/v1/providers?offset=%d&limit=%d", r.baseURL, offset, limit)
		resp, err := r.client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to get provider list at offset %d: %w", offset, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry returned status %d for provider list at offset %d", resp.StatusCode, offset)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		var providerList common.ProviderList
		if err := json.Unmarshal(body, &providerList); err != nil {
			return nil, fmt.Errorf("failed to parse provider list: %w", err)
		}

		if len(providerList.Providers) == 0 {
			break // No more providers
		}

		allProviders = append(allProviders, providerList.Providers...)
		r.logger.Debug("Found %d providers in this batch (total: %d)", len(providerList.Providers), len(allProviders))

		// If we got less than the limit, we've reached the end
		if len(providerList.Providers) < limit {
			break
		}

		offset += limit
	}

	r.logger.Info("Discovery complete: found %d total providers", len(allProviders))
	return allProviders, nil
}

// GetProviderList retrieves all available providers from the registry (legacy method)
func (r *RegistryClient) GetProviderList() (*common.ProviderList, error) {
	providers, err := r.DiscoverAllProviders()
	if err != nil {
		return nil, err
	}

	return &common.ProviderList{
		Providers: providers,
	}, nil
}

// GetProviderVersions retrieves all versions for a specific provider
func (r *RegistryClient) GetProviderVersions(namespace, name string) (*common.ProviderVersions, error) {
	url := fmt.Sprintf("%s/v1/providers/%s/%s/versions", r.baseURL, namespace, name)

	resp, err := r.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider versions for %s/%s: %w", namespace, name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("provider %s/%s not found in registry", namespace, name)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for provider %s/%s versions", resp.StatusCode, namespace, name)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var versions common.ProviderVersions
	if err := json.Unmarshal(body, &versions); err != nil {
		return nil, fmt.Errorf("failed to parse provider versions: %w", err)
	}

	return &versions, nil
}

// GetProviderPackage retrieves package information for a specific provider version and platform
func (r *RegistryClient) GetProviderPackage(ctx context.Context, namespace, name, version, os, arch string) (*common.ProviderPackage, error) {
	url := fmt.Sprintf("%s/v1/providers/%s/%s/%s/download/%s/%s", r.baseURL, namespace, name, version, os, arch)

	resp, err := r.client.GetWithContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider package for %s/%s %s %s/%s: %w", namespace, name, version, os, arch, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("provider package %s/%s %s %s/%s not found in registry", namespace, name, version, os, arch)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for provider package %s/%s %s %s/%s", resp.StatusCode, namespace, name, version, os, arch)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var pkg common.ProviderPackage
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse provider package: %w", err)
	}

	return &pkg, nil
}

// DownloadFile downloads a file from the given URL to the specified path
func (r *RegistryClient) DownloadFile(ctx context.Context, url, destPath string) error {
	r.logger.Debug("Downloading file from %s to %s", url, destPath)

	resp, err := r.client.GetWithContext(ctx, url)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d for URL %s", resp.StatusCode, url)
	}

	return r.saveFile(resp.Body, destPath)
}

// saveFile saves the content from reader to the specified file path
func (r *RegistryClient) saveFile(reader io.Reader, destPath string) error {
	r.logger.Debug("saveFile: starting for %s", destPath)
	// Create directory if it doesn't exist
	dir := filepath.Dir(destPath)
	if err := createDirIfNotExists(dir); err != nil {
		r.logger.Error("saveFile: failed to create directory %s: %v", dir, err)
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create temporary file first
	tempPath := destPath + ".tmp"
	r.logger.Debug("saveFile: creating temp file %s", tempPath)
	file, err := createFile(tempPath)
	if err != nil {
		r.logger.Error("saveFile: failed to create temporary file %s: %v", tempPath, err)
		return fmt.Errorf("failed to create temporary file %s: %w", tempPath, err)
	}

	// Copy content
	r.logger.Debug("saveFile: copying content to %s", tempPath)
	_, err = io.Copy(file, reader)
	closeErr := file.Close()

	if err != nil {
		r.logger.Error("saveFile: failed to write file content to %s: %v", tempPath, err)
		removeFile(tempPath) // Clean up on error
		return fmt.Errorf("failed to write file content: %w", err)
	}

	if closeErr != nil {
		r.logger.Error("saveFile: failed to close file %s: %v", tempPath, closeErr)
		removeFile(tempPath) // Clean up on error
		return fmt.Errorf("failed to close file: %w", closeErr)
	}

	// Rename temporary file to final destination
	r.logger.Debug("saveFile: renaming temp file %s to %s", tempPath, destPath)
	if err := renameFile(tempPath, destPath); err != nil {
		r.logger.Error("saveFile: failed to rename temporary file %s to %s: %v", tempPath, destPath, err)
		removeFile(tempPath) // Clean up on error
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	r.logger.Debug("saveFile: finished for %s", destPath)
	return nil
}

// GetProviderPath returns the file path for a provider based on Terraform registry structure
func (r *RegistryClient) GetProviderPath(basePath, namespace, name, version, os, arch, filename string) string {
	// Network Mirror Protocol: all versions and platforms in one folder
	// Path: <download-path>/registry.terraform.io/namespace/name/filename
	return filepath.Join(basePath, "registry.terraform.io", namespace, name, filename)
}

// GetProviderVersionJSONPath returns the path for a provider version metadata json
func (r *RegistryClient) GetProviderVersionJSONPath(basePath, namespace, name, version string) string {
	// Path: <download-path>/registry.terraform.io/namespace/name/version.json
	return filepath.Join(basePath, "registry.terraform.io", namespace, name, version+".json")
}

// Close closes the registry client
func (r *RegistryClient) Close() error {
	return r.client.Close()
}

// Helper functions that can be mocked for testing
var (
	createDirIfNotExists = func(path string) error {
		return createDirAll(path, 0755)
	}
	createFile = func(path string) (io.WriteCloser, error) {
		return createFileHandle(path)
	}
	removeFile = func(path string) error {
		return removeFileHandle(path)
	}
	renameFile = func(oldPath, newPath string) error {
		return renameFileHandle(oldPath, newPath)
	}
)

// IsProviderPath checks if a given path matches the expected provider structure
func IsProviderPath(path string) bool {
	// Expected structure: namespace/name/version/os_arch/filename
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	if len(parts) < 5 {
		return false
	}

	// Check if the 4th component (os_arch) contains an underscore
	osArch := parts[len(parts)-2]
	return strings.Contains(osArch, "_")
}
