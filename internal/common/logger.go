package common

import (
	"log"
	"os"
)

// Logger represents a structured logger
type Logger struct {
	infoLogger  *log.Logger
	errorLogger *log.Logger
	debugLogger *log.Logger
}

// NewLogger creates a new logger instance
func NewLogger() *Logger {
	return &Logger{
		infoLogger:  log.New(os.Stdout, "[INFO] ", log.LstdFlags),
		errorLogger: log.New(os.Stderr, "[ERROR] ", log.LstdFlags),
		debugLogger: log.New(os.Stdout, "[DEBUG] ", log.LstdFlags),
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...any) {
	l.infoLogger.Printf(format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...any) {
	l.infoLogger.Printf("[WARN] "+format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...any) {
	l.errorLogger.Printf(format, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...any) {
	if os.Getenv("DEBUG") != "" {
		l.debugLogger.Printf(format, args...)
	}
}

// Fatal logs a fatal error and exits
func (l *Logger) Fatal(format string, args ...any) {
	l.errorLogger.Printf(format, args...)
	os.Exit(1)
}
