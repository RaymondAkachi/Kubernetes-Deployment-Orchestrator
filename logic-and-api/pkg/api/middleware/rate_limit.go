// pkg/api/middleware/ratelimit.go
package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

type IPRateLimiter struct {
    ips map[string]*rate.Limiter
    mu  *sync.RWMutex
    r   rate.Limit
    b   int
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
    return &IPRateLimiter{
        ips: make(map[string]*rate.Limiter),
        mu:  &sync.RWMutex{},
        r:   r,
        b:   b,
    }
}

func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
    i.mu.Lock()
    defer i.mu.Unlock()
    
    limiter := rate.NewLimiter(i.r, i.b)
    i.ips[ip] = limiter
    
    return limiter
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
    i.mu.RLock()
    limiter, exists := i.ips[ip]
    i.mu.RUnlock()
    
    if !exists {
        return i.AddIP(ip)
    }
    
    return limiter
}

func RateLimitMiddleware(cfg *config.RateLimitConfig) gin.HandlerFunc {
    if !cfg.Enabled {
        return func(c *gin.Context) {
            c.Next()
        }
    }
    
    limiter := NewIPRateLimiter(rate.Limit(cfg.RPS), cfg.Burst)
    
    return func(c *gin.Context) {
        ip := c.ClientIP()
        l := limiter.GetLimiter(ip)
        
        if !l.Allow() {
            c.JSON(http.StatusTooManyRequests, gin.H{
                "error": "Rate limit exceeded",
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}