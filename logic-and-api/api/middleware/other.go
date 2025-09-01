package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)


func TimeoutMiddleware(config *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {

		//TODO: Check this line below potential
        timeoutDuration, err := time.ParseDuration(strconv.Itoa(int(config.Kubernetes.Timeout)))
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid timeout format: %v", err)})
            c.Abort()
            return
        }

        ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutDuration)
        defer cancel()

        c.Request = c.Request.WithContext(ctx)

        c.Next()

        if ctx.Err() == context.DeadlineExceeded {
            c.JSON(http.StatusRequestTimeout, gin.H{"error": "request timed out"})
            c.Abort()
            return
        }
    }
}

func NonBlockingRateLimiter(limit float64, burst int) gin.HandlerFunc {

    limiter := rate.NewLimiter(rate.Limit(limit), burst)

    return func(c *gin.Context) {
        if !limiter.Allow() {
            c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
            c.Abort() // Stop the request from going to the next handler.
            return
        }
        c.Next()
    }
}