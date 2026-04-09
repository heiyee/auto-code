package logging

import (
	"os"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// LevelEnvKey is the environment variable name used to configure zap level.
	LevelEnvKey = "AUTO_CODE_LOG_LEVEL"
)

var globalLogger atomic.Pointer[zap.Logger]

// init seeds global logger with a no-op logger before explicit initialization.
func init() {
	globalLogger.Store(zap.NewNop())
}

// Init configures the global zap logger from environment defaults.
func Init() {
	InitWithLevel(os.Getenv(LevelEnvKey))
}

// InitWithLevel configures the global zap logger with one explicit level string.
func InitWithLevel(rawLevel string) {
	logger, level, err := buildLogger(rawLevel)
	if err != nil {
		fallback := zap.NewExample()
		setLogger(fallback)
		fallback.Warn(
			"initialize zap logger failed, fallback to example logger",
			zap.Error(err),
			zap.String("requested_level", strings.TrimSpace(rawLevel)),
		)
		return
	}

	setLogger(logger)
	logger.Info("logger initialized", zap.String("level", level.String()))
}

// L returns the current global zap logger.
func L() *zap.Logger {
	if logger := globalLogger.Load(); logger != nil {
		return logger
	}
	return zap.NewNop()
}

// Named returns one component logger based on global logger.
func Named(component string) *zap.Logger {
	component = strings.TrimSpace(component)
	if component == "" {
		return L()
	}
	return L().Named(component)
}

// Sync flushes buffered logs to configured outputs.
func Sync() {
	_ = L().Sync()
}

// buildLogger creates one production logger and resolved level.
func buildLogger(rawLevel string) (*zap.Logger, zapcore.Level, error) {
	level := parseLevel(rawLevel)

	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := cfg.Build(zap.AddCaller())
	if err != nil {
		return nil, level, err
	}
	return logger, level, nil
}

// setLogger replaces the current global logger safely.
func setLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	previous := globalLogger.Swap(logger)
	if previous != nil {
		_ = previous.Sync()
	}
}

// parseLevel maps one textual log level into zap level with info fallback.
func parseLevel(rawLevel string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(rawLevel)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}
