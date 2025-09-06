package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseEnvInt parses an integer from environment variable
func ParseEnvInt(envVar string, defaultValue int) (int, error) {
	value := os.Getenv(envVar)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid integer value for %s: %v", envVar, err)
	}

	return parsed, nil
}

// ParseEnvBool parses a boolean from environment variable
func ParseEnvBool(envVar string, defaultValue bool) (bool, error) {
	value := strings.ToLower(os.Getenv(envVar))
	if value == "" {
		return defaultValue, nil
	}

	switch value {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return defaultValue, fmt.Errorf("invalid boolean value for %s: %s", envVar, value)
	}
}

// ParseEnvDuration parses a duration from environment variable
func ParseEnvDuration(envVar string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(envVar)
	if value == "" {
		return defaultValue, nil
	}

	// Try to parse as hours first (for backward compatibility)
	if hours, err := strconv.Atoi(value); err == nil {
		return time.Duration(hours) * time.Hour, nil
	}

	// Try to parse as duration string
	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid duration value for %s: %v", envVar, err)
	}

	return duration, nil
}

// GetEnvWithDefault returns environment variable value or default if not set
func GetEnvWithDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}
