package common

import (
	"time"
)

// Provider represents a Terraform provider
type Provider struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Hostname  string `json:"hostname,omitempty"`
}

// Version represents a provider version
type Version struct {
	Version   string            `json:"version"`
	Platforms []Platform        `json:"platforms"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Platform represents a provider platform
type Platform struct {
	OS                  string      `json:"os"`
	Arch                string      `json:"arch"`
	Filename            string      `json:"filename"`
	DownloadURL         string      `json:"download_url"`
	SHASumsURL          string      `json:"shasums_url"`
	SHASumsSignatureURL string      `json:"shasums_signature_url"`
	Shasum              string      `json:"shasum"`
	SigningKeys         SigningKeys `json:"signing_keys,omitempty"`
}

// SigningKeys represents GPG signing keys
type SigningKeys struct {
	GPGPublicKeys []GPGPublicKey `json:"gpg_public_keys,omitempty"`
}

// GPGPublicKey represents a GPG public key
type GPGPublicKey struct {
	KeyID          string `json:"key_id"`
	ASCIIArmor     string `json:"ascii_armor"`
	TrustSignature string `json:"trust_signature,omitempty"`
	Source         string `json:"source,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`
}

// ProviderVersions represents the response from provider versions API
type ProviderVersions struct {
	Versions []Version `json:"versions"`
}

// ProviderPackage represents the response from provider download API
type ProviderPackage struct {
	Protocols           []string    `json:"protocols"`
	OS                  string      `json:"os"`
	Arch                string      `json:"arch"`
	Filename            string      `json:"filename"`
	DownloadURL         string      `json:"download_url"`
	SHASumsURL          string      `json:"shasums_url"`
	SHASumsSignatureURL string      `json:"shasums_signature_url"`
	Shasum              string      `json:"shasum"`
	SigningKeys         SigningKeys `json:"signing_keys"`
}

// DownloadedBinary represents a HashiCorp binary that has been downloaded
type DownloadedBinary struct {
	Tool       string    `json:"tool"`
	FilePath   string    `json:"file_path"` // относительный путь от download-path
	Platforms  []string  `json:"platforms"`
	Versions   []string  `json:"versions"`
	Downloaded time.Time `json:"downloaded"`
}

// ProviderList represents the response from providers list API
type ProviderList struct {
	Providers []ProviderListItem `json:"providers"`
}

// ProviderListItem represents a single provider in the list
type ProviderListItem struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DownloadedProvider represents a provider that has been downloaded
type DownloadedProvider struct {
	Provider   Provider  `json:"provider"`
	Version    string    `json:"version"`
	Platform   Platform  `json:"platform"`
	FilePath   string    `json:"file_path"`
	Downloaded time.Time `json:"downloaded"`
}

// RegistryConfig represents the configuration for registry operations
type RegistryConfig struct {
	BaseURL    string
	ProxyURL   string
	UserAgent  string
	Timeout    time.Duration
	MaxRetries int
}

// ServerConfig represents the HTTP server configuration
type ServerConfig struct {
	ListenHost string
	ListenPort int
	Hostname   string
	EnableTLS  bool
	TLSCert    string
	TLSKey     string
	DataPath   string
}

// DownloaderConfig represents the downloader configuration
type DownloaderConfig struct {
	ProxyURL         string
	CheckPeriod      time.Duration
	DownloadPath     string
	MaxConcurrent    int
	ProviderFilter   string
	PlatformFilter   string
	MaxAttempts      int           // Maximum download attempts (default: 5)
	DownloadTimeout  time.Duration // Download timeout per attempt (default: 180s)
	DownloadBinaries string        // Optional: filter for downloading HashiCorp binaries (e.g. "consul>1.21.3")
}

// ErrorResponse represents an error response from the registry
type ErrorResponse struct {
	Errors []ErrorDetail `json:"errors"`
}

// ErrorDetail represents details of an error
type ErrorDetail struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// WellKnownConfig represents the .well-known/terraform.json configuration
type WellKnownConfig struct {
	ProvidersV1 string `json:"providers.v1"`
}

// ServiceDiscovery represents the service discovery response
type ServiceDiscovery struct {
	ProvidersV1 string `json:"providers.v1"`
}

const (
	// TerraformRegistryURL is the official Terraform registry URL
	TerraformRegistryURL = "https://registry.terraform.io"

	// UserAgent for HTTP requests
	UserAgent = "terraform-mirror/1.0"

	// Default timeout for HTTP requests
	DefaultTimeout = 30 * time.Second

	// Default number of retries
	DefaultMaxRetries = 3

	// Default concurrent downloads
	DefaultMaxConcurrent = 5
)

// Common supported platforms
var SupportedPlatforms = []Platform{
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
	{OS: "linux", Arch: "386"},
	{OS: "darwin", Arch: "amd64"},
	{OS: "darwin", Arch: "arm64"},
	{OS: "windows", Arch: "amd64"},
	{OS: "windows", Arch: "386"},
	{OS: "freebsd", Arch: "amd64"},
	{OS: "freebsd", Arch: "386"},
}
