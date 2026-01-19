package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap logger for structured logging
type Logger struct {
	*zap.SugaredLogger
}

// New creates a new logger with the specified level and format
func New(level string, format string) (*Logger, error) {
	var config zap.Config

	if format == "json" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Set log level
	logLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		logLevel = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(logLevel)

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{
		SugaredLogger: logger.Sugar(),
	}, nil
}

// NewDefault creates a logger with default settings (info level, text format)
func NewDefault() *Logger {
	logger, _ := New("info", "text")
	return logger
}

// With creates a child logger with additional fields
func (l *Logger) With(args ...interface{}) *Logger {
	return &Logger{
		SugaredLogger: l.SugaredLogger.With(args...),
	}
}
