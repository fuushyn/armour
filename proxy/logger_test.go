package proxy

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLoggerDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LogDebug,
		logger: log.New(buf, "", 0),
	}

	logger.Debug("test message %s", "value")
	output := buf.String()

	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("expected [DEBUG] in output, got: %s", output)
	}
	if !strings.Contains(output, "test message value") {
		t.Errorf("expected message in output, got: %s", output)
	}
}

func TestLoggerInfo(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LogInfo,
		logger: log.New(buf, "", 0),
	}

	logger.Info("info message")
	output := buf.String()

	if !strings.Contains(output, "[INFO]") {
		t.Errorf("expected [INFO] in output, got: %s", output)
	}
}

func TestLoggerFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LogWarn,
		logger: log.New(buf, "", 0),
	}

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")

	output := buf.String()

	if strings.Contains(output, "debug message") {
		t.Errorf("debug should be filtered, got: %s", output)
	}
	if strings.Contains(output, "info message") {
		t.Errorf("info should be filtered, got: %s", output)
	}
	if !strings.Contains(output, "warn message") {
		t.Errorf("warn should be present, got: %s", output)
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		level    string
		expected LogLevel
	}{
		{"debug", LogDebug},
		{"info", LogInfo},
		{"warn", LogWarn},
		{"error", LogError},
		{"invalid", LogInfo},
	}

	for _, test := range tests {
		logger := NewLogger(test.level)
		if logger.level != test.expected {
			t.Errorf("level %s: expected %d, got %d", test.level, test.expected, logger.level)
		}
	}
}
