// pkg/api/middleware/validation.go
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func ValidationMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Set("validator", validate)
        c.Next()
    }
}

func ValidateJSON(obj interface{}) gin.HandlerFunc {
    return func(c *gin.Context) {
        if err := c.ShouldBindJSON(obj); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error":   "Invalid JSON format",
                "details": err.Error(),
            })
            c.Abort()
            return
        }
        
        if err := validate.Struct(obj); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error":   "Validation failed",
                "details": err.Error(),
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}