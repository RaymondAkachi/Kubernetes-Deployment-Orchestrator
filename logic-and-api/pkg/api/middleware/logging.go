// pkg/api/middleware/logging.go
package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func LoggingMiddleware(logger *zap.Logger) gin.HandlerFunc {
    return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
        logger.Info("api request",
            zap.String("method", param.Method),
            zap.String("path", param.Path),
            zap.Int("status", param.StatusCode),
            zap.Duration("latency", param.Latency),
            zap.String("client_ip", param.ClientIP),
            zap.String("user_agent", param.Request.UserAgent()),
        )
        return ""
    })
}

