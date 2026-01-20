package proxy

import (
	"log"
	"os"
	"strings"
)

type LogLevel int

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

type Logger struct {
	level  LogLevel
	logger *log.Logger
}

func NewLogger(level string) *Logger {
	var logLevel LogLevel
	switch strings.ToLower(level) {
	case "debug":
		logLevel = LogDebug
	case "warn":
		logLevel = LogWarn
	case "error":
		logLevel = LogError
	default:
		logLevel = LogInfo
	}

	return &Logger{
		level:  logLevel,
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (l *Logger) Debug(msg string, args ...interface{}) {
	if l.level <= LogDebug {
		l.logger.Printf("[DEBUG] "+msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...interface{}) {
	if l.level <= LogInfo {
		l.logger.Printf("[INFO] "+msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	if l.level <= LogWarn {
		l.logger.Printf("[WARN] "+msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...interface{}) {
	if l.level <= LogError {
		l.logger.Printf("[ERROR] "+msg, args...)
	}
}

func (l *Logger) Logf(format string, args ...interface{}) {
	l.logger.Printf(format, args...)
}
