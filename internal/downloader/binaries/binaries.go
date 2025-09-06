package binaries

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"tf-mirror/internal/common"
	"time"

	"golang.org/x/net/proxy"
)

// Platform describes a target OS/Arch for downloading binaries
type Platform struct {
	OS   string
	Arch string
}

// BinaryFilter describes a tool and minimal version to download
type BinaryFilter struct {
	Tool       string
	MinVersion string
}

// ParseBinaryFilter parses a filter string like "consul>1.21.3,nomad>1.6.0"
func ParseBinaryFilter(filter string) ([]BinaryFilter, error) {
	var result []BinaryFilter
	if filter == "" {
		return result, nil
	}
	parts := strings.Split(filter, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sub := strings.Split(part, ">")
		if len(sub) != 2 {
			return nil, fmt.Errorf("invalid binary filter format: %s", part)
		}
		result = append(result, BinaryFilter{
			Tool:       strings.TrimSpace(sub[0]),
			MinVersion: strings.TrimSpace(sub[1]),
		})
	}
	return result, nil
}

// DownloadHashiCorpBinaries downloads binaries from releases.hashicorp.com
// downloadPath: root directory for binaries
// filters: parsed list of BinaryFilter
// platforms: list of platforms to download (os/arch)
// proxyURL: optional proxy URL (http/https/socks5)
// Returns: slice of DownloadedBinary with metadata about downloaded binaries
func DownloadHashiCorpBinaries(downloadPath string, filters []BinaryFilter, platforms []Platform, logger func(format string, args ...interface{}), proxyURL ...string) ([]common.DownloadedBinary, error) {
	var downloaded []common.DownloadedBinary
	now := time.Now().UTC()

	var proxy string
	if len(proxyURL) > 0 {
		proxy = proxyURL[0]
	}

	httpClient, err := buildProxyHTTPClient(proxy)
	if err != nil {
		return nil, fmt.Errorf("failed to build proxy http client: %w", err)
	}

	for _, filter := range filters {
		logger("Processing tool: %s (min version: %s)", filter.Tool, filter.MinVersion)
		versions, err := fetchAvailableVersionsWithClient(filter.Tool, httpClient)
		if err != nil {
			logger("  Failed to fetch versions for %s: %v", filter.Tool, err)
			continue
		}
		// semver-фильтрация через FilterVersionsByMin
		filteredVersions := common.FilterVersionsByMin(versions, filter.MinVersion)
		// Собираем map[platform] -> []version для этого tool
		type binKey struct {
			platform string
			filePath string
		}
		binMap := make(map[binKey]struct {
			versions   map[string]struct{}
			downloaded time.Time
		})
		for _, version := range filteredVersions {
			for _, platform := range platforms {
				platformStr := fmt.Sprintf("%s_%s", platform.OS, platform.Arch)
				zipName := fmt.Sprintf("%s_%s_%s_%s.zip", filter.Tool, version, platform.OS, platform.Arch)
				url := fmt.Sprintf("https://releases.hashicorp.com/%s/%s/%s", filter.Tool, version, zipName)
				destDir := filepath.Join(downloadPath, filter.Tool)
				destPath := filepath.Join(destDir, zipName)
				relPath := filepath.Join(filter.Tool, zipName)
				key := binKey{platform: platformStr, filePath: relPath}
				if _, ok := binMap[key]; !ok {
					binMap[key] = struct {
						versions   map[string]struct{}
						downloaded time.Time
					}{versions: make(map[string]struct{}), downloaded: now}
				}
				if fileExists(destPath) {
					logger("  Skipping (already exists): %s", destPath)
					b := binMap[key]
					b.versions[version] = struct{}{}
					binMap[key] = b
					continue
				}
				if err := os.MkdirAll(destDir, 0755); err != nil {
					logger("  Failed to create dir %s: %v", destDir, err)
					continue
				}
				logger("  Downloading: %s", url)
				if err := downloadFileWithClient(url, destPath, httpClient); err != nil {
					logger("    Failed: %v", err)
				} else {
					logger("    Success: %s", destPath)
					b := binMap[key]
					b.versions[version] = struct{}{}
					binMap[key] = b
				}
			}
		}
		// Собираем результат
		for key, val := range binMap {
			var versions []string
			for v := range val.versions {
				versions = append(versions, v)
			}
			downloaded = append(downloaded, common.DownloadedBinary{
				Tool:       filter.Tool,
				FilePath:   key.filePath,
				Platforms:  []string{key.platform},
				Versions:   versions,
				Downloaded: val.downloaded,
			})
		}
	}
	return downloaded, nil
}

// fetchAvailableVersions scrapes the list of available versions for a tool from releases.hashicorp.com using default http.Get
func fetchAvailableVersions(tool string) ([]string, error) {
	return fetchAvailableVersionsWithClient(tool, http.DefaultClient)
}

// fetchAvailableVersionsWithClient allows using a custom http.Client (with proxy)
func fetchAvailableVersionsWithClient(tool string, client *http.Client) ([]string, error) {
	url := fmt.Sprintf("https://releases.hashicorp.com/%s/", tool)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d for %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Find all href="/tool/x.y.z/"
	re := regexp.MustCompile(fmt.Sprintf(`href="/%s/([0-9]+\.[0-9]+\.[0-9]+)/"`, regexp.QuoteMeta(tool)))
	matches := re.FindAllStringSubmatch(string(body), -1)
	versions := make([]string, 0, len(matches))
	for _, match := range matches {
		versions = append(versions, match[1])
	}
	return versions, nil
}

// compareVersions returns -1 if a < b, 0 if a == b, 1 if a > b
// compareVersions больше не нужен, фильтрация теперь через common.FilterVersionsByMin

// sortVersions sorts versions in ascending order
// sortVersions больше не нужен, фильтрация теперь через common.FilterVersionsByMin

// downloadFile downloads a file from url to destPath using default http.Get
func downloadFile(url, destPath string) error {
	return downloadFileWithClient(url, destPath, http.DefaultClient)
}

// downloadFileWithClient downloads a file using a custom http.Client (with proxy)
func downloadFileWithClient(url, destPath string, client *http.Client) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code %d for %s", resp.StatusCode, url)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildProxyHTTPClient builds an http.Client with proxy support (http, https, socks5)
func buildProxyHTTPClient(proxyStr string) (*http.Client, error) {
	if proxyStr == "" {
		return http.DefaultClient, nil
	}
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	transport := &http.Transport{}
	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5":
		// socks5 proxy support
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}
		transport.Dial = dialer.Dial
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
	return &http.Client{Transport: transport}, nil
}

// SupportedPlatforms returns a default list of platforms for HashiCorp binaries
func SupportedPlatforms() []Platform {
	return []Platform{
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm"},
		{OS: "linux", Arch: "arm64"},
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm"},
		{OS: "windows", Arch: "arm64"},
		{OS: "freebsd", Arch: "amd64"},
	}
}

// For testing: pretty print JSON
func prettyPrint(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
