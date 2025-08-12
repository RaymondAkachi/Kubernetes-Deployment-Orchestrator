// pkg/logging/logger.go
package logging

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

func NewLogger(cfg *config.LoggingConfig) (*zap.Logger, error) {
    level, err := zapcore.ParseLevel(cfg.Level)
    if err != nil {
        return nil, fmt.Errorf("invalid log level: %w", err)
    }
    
    config := zap.Config{
        Level:       zap.NewAtomicLevelAt(level),
        Development: false,
        // Encoding:    cfg.Format,
        EncoderConfig: zapcore.EncoderConfig{
            TimeKey:        "timestamp",
            LevelKey:       "level",
            NameKey:        "logger",
            CallerKey:      "caller",
            MessageKey:     "message",
            StacktraceKey:  "stacktrace",
            LineEnding:     zapcore.DefaultLineEnding,
            EncodeLevel:    zapcore.LowercaseLevelEncoder,
            EncodeTime:     zapcore.ISO8601TimeEncoder,
            EncodeDuration: zapcore.StringDurationEncoder,
            EncodeCaller:   zapcore.ShortCallerEncoder,
        },
        OutputPaths:      []string{cfg.Path},
        ErrorOutputPaths: []string{"stderr"},
    }
    
    logger, err := config.Build()
    if err != nil {
        return nil, fmt.Errorf("failed to build logger: %w", err)
    }
    
    return logger, nil
}