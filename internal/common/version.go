package common

import (
	"fmt"
	"runtime"
)

// Build information - set at build time via ldflags
var (
	BuildVersion = "dev"
	Commit       = "unknown"
	BuildTime    = "unknown"
	GoVersion    = runtime.Version()
)

// VersionInfo represents version information
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// GetVersionInfo returns version information
func GetVersionInfo() *VersionInfo {
	return &VersionInfo{
		Version:   BuildVersion,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: GoVersion,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// GetVersionString returns a formatted version string
func GetVersionString() string {
	if Commit != "unknown" && len(Commit) > 7 {
		return fmt.Sprintf("%s-%s", BuildVersion, Commit[:7])
	}
	return BuildVersion
}

// GetFullVersionString returns a detailed version string
func GetFullVersionString() string {
	return fmt.Sprintf("Version: %s\nCommit: %s\nBuild Time: %s\nGo Version: %s\nPlatform: %s/%s",
		BuildVersion, Commit, BuildTime, GoVersion, runtime.GOOS, runtime.GOARCH)
}
