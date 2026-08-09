package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(logLevel string) (*zap.Logger, error) {
	var cfg zap.Config
	if os.Getenv("APP_ENV") == "production" {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.LowercaseColorLevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
		cfg.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	}
	if logLevel != "" {
		if level, err := zapcore.ParseLevel(logLevel); err == nil {
			cfg.Level = zap.NewAtomicLevelAt(level)
		}
	}
	return cfg.Build()
}
