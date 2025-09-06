package common

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
)

// ProviderFilterItem stores filter info for a provider
type ProviderFilterItem struct {
	Namespace  string
	Name       string
	MinVersion string // "" если не указана
}

// ProviderFilter represents a filter for providers
type ProviderFilter struct {
	providers map[string]ProviderFilterItem
	enabled   bool
}

// PlatformFilter represents a filter for platforms
type PlatformFilter struct {
	platforms map[string]bool
	enabled   bool
}

// NewProviderFilter creates a new provider filter from comma-separated string
// Supports format: namespace/name>version
func NewProviderFilter(filterString string) (*ProviderFilter, error) {
	filter := &ProviderFilter{
		providers: make(map[string]ProviderFilterItem),
		enabled:   false,
	}

	if filterString == "" {
		return filter, nil
	}

	providers := strings.Split(filterString, ",")
	for _, entry := range providers {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ">")
		provider := parts[0]
		minVersion := ""
		if len(parts) > 1 {
			minVersion = strings.TrimSpace(parts[1])
		}
		nsName := strings.Split(provider, "/")
		if len(nsName) != 2 || nsName[0] == "" || nsName[1] == "" {
			return nil, fmt.Errorf("invalid provider format '%s', expected 'namespace/name' or 'namespace/name>version'", entry)
		}
		key := fmt.Sprintf("%s/%s", nsName[0], nsName[1])
		filter.providers[key] = ProviderFilterItem{
			Namespace:  nsName[0],
			Name:       nsName[1],
			MinVersion: minVersion,
		}
		filter.enabled = true
	}

	return filter, nil
}

// NewPlatformFilter creates a new platform filter from comma-separated string
func NewPlatformFilter(filterString string) (*PlatformFilter, error) {
	filter := &PlatformFilter{
		platforms: make(map[string]bool),
		enabled:   false,
	}

	if filterString == "" {
		return filter, nil
	}

	platforms := strings.Split(filterString, ",")
	for _, platform := range platforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}

		// Validate platform format (os_arch)
		parts := strings.Split(platform, "_")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid platform format '%s', expected 'os_arch'", platform)
		}

		filter.platforms[platform] = true
		filter.enabled = true
	}

	return filter, nil
}

// IsEnabled returns true if the filter is enabled (has filters configured)
func (f *ProviderFilter) IsEnabled() bool {
	return f.enabled
}

// IsEnabled returns true if the filter is enabled (has filters configured)
func (f *PlatformFilter) IsEnabled() bool {
	return f.enabled
}

// ShouldInclude returns true if the provider should be included (by name only)
func (f *ProviderFilter) ShouldInclude(namespace, name string) bool {
	if !f.enabled {
		return true // No filter means include all
	}
	provider := fmt.Sprintf("%s/%s", namespace, name)
	_, ok := f.providers[provider]
	return ok
}

// GetMinVersion returns the minVersion for a provider, or "" if not set
func (f *ProviderFilter) GetMinVersion(namespace, name string) string {
	if !f.enabled {
		return ""
	}
	provider := fmt.Sprintf("%s/%s", namespace, name)
	item, ok := f.providers[provider]
	if !ok {
		return ""
	}
	return item.MinVersion
}

// ShouldInclude returns true if the platform should be included
func (f *PlatformFilter) ShouldInclude(os, arch string) bool {
	if !f.enabled {
		return true // No filter means include all
	}

	platform := fmt.Sprintf("%s_%s", os, arch)
	return f.platforms[platform]
}

// GetProviders returns the list of filtered providers (keys)
func (f *ProviderFilter) GetProviders() []string {
	if !f.enabled {
		return nil
	}
	providers := make([]string, 0, len(f.providers))
	for provider := range f.providers {
		providers = append(providers, provider)
	}
	return providers
}

// GetProviderItems returns the list of ProviderFilterItem
func (f *ProviderFilter) GetProviderItems() []ProviderFilterItem {
	if !f.enabled {
		return nil
	}
	items := make([]ProviderFilterItem, 0, len(f.providers))
	for _, item := range f.providers {
		items = append(items, item)
	}
	return items
}

// GetPlatforms returns the list of filtered platforms
func (f *PlatformFilter) GetPlatforms() []string {
	if !f.enabled {
		return nil
	}

	platforms := make([]string, 0, len(f.platforms))
	for platform := range f.platforms {
		platforms = append(platforms, platform)
	}
	return platforms
}

// String returns a string representation of the provider filter
func (f *ProviderFilter) String() string {
	if !f.enabled {
		return "all providers"
	}

	providers := f.GetProviders()
	return strings.Join(providers, ", ")
}

// String returns a string representation of the platform filter
func (f *PlatformFilter) String() string {
	if !f.enabled {
		return "all platforms"
	}

	platforms := f.GetPlatforms()
	return strings.Join(platforms, ", ")
}

// Count returns the number of providers in the filter
func (f *ProviderFilter) Count() int {
	return len(f.providers)
}

// FilterVersionsByMin returns only versions >= minVersion (semver), or all if minVersion is ""
func FilterVersionsByMin(versions []string, minVersion string) []string {
	if minVersion == "" {
		return versions
	}
	minVer, err := semver.ParseTolerant(minVersion)
	if err != nil {
		// Если minVersion некорректна, возвращаем все версии
		return versions
	}
	var filtered []string
	for _, v := range versions {
		ver, err := semver.ParseTolerant(v)
		if err != nil {
			continue // пропускаем некорректные версии
		}
		if ver.GTE(minVer) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// Count returns the number of platforms in the filter
func (f *PlatformFilter) Count() int {
	return len(f.platforms)
}
