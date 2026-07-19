// Package logger provides a tiny leveled logger wrapper used across the service.
// It is intentionally dependency-free (stdlib log) so it can be swapped for a
// structured logger (zap/zerolog) later without touching call sites much.
package logger

import (
	"log"
	"os"
)

// Logger is a minimal leveled logger.
type Logger struct {
	*log.Logger
}

// New returns a Logger writing to stdout with date/time and short file info.
func New() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

// Infof logs an informational message.
func (l *Logger) Infof(format string, args ...any) {
	l.Printf("[INFO]  "+format, args...)
}

// Warnf logs a warning message.
func (l *Logger) Warnf(format string, args ...any) {
	l.Printf("[WARN]  "+format, args...)
}

// Errorf logs an error message.
func (l *Logger) Errorf(format string, args ...any) {
	l.Printf("[ERROR] "+format, args...)
}

// Fatalf logs a fatal message and exits with status 1.
func (l *Logger) Fatalf(format string, args ...any) {
	l.Logger.Fatalf("[FATAL] "+format, args...)
}
