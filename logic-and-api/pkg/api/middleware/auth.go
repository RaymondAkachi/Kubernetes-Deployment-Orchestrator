// pkg/api/middleware/auth.go
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

func AuthMiddleware(cfg *config.AuthConfig, logger *zap.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        if !cfg.Enabled {
            c.Next()
            return
        }
        
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            logger.Warn("missing authorization header", zap.String("path", c.Request.URL.Path))
            c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
            c.Abort()
            return
        }
        
        // Extract token from "Bearer <token>" format
        parts := strings.SplitN(authHeader, " ", 2)
        if len(parts) != 2 || parts[0] != "Bearer" {
            logger.Warn("invalid authorization header format", zap.String("path", c.Request.URL.Path))
            c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
            c.Abort()
            return
        }
        
        token := parts[1]
        
        // Validate token based on configured auth type
        switch cfg.Type {
        case "token":
            if !isValidToken(token, cfg.Tokens) {
                logger.Warn("invalid token", zap.String("path", c.Request.URL.Path))
                c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
                c.Abort()
                return
            }
        case "jwt":
            // JWT validation would go here
            // For now, implement simple token validation
            if !isValidToken(token, cfg.Tokens) {
                logger.Warn("invalid JWT token", zap.String("path", c.Request.URL.Path))
                c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid JWT token"})
                c.Abort()
                return
            }
        default:
            logger.Error("unsupported auth type", zap.String("type", cfg.Type))
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal authentication error"})
            c.Abort()
            return
        }
        
        c.Next()
    }
}

func isValidToken(token string, validTokens []string) bool {
    for _, validToken := range validTokens {
        if token == validToken {
            return true
        }
    }
    return false
}
