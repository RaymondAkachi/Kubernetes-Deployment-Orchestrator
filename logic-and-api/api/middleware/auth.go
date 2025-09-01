package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/logging"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type contextKey string

const UserKey contextKey = "user"

// ResourcePermission defines a Kubernetes resource and action
type ResourcePermission struct {
	APIGroup  string
	Resource  string
	Verb      string
	Namespace string
}

// Auth validates Kubernetes tokens using TokenReview
func Auth(clientset *kubernetes.Clientset, cfg *config.Config, logger *zap.Logger, auditLogger *logging.AuthLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// if cfg.Security.Auth.Type != "k8s_token" {
		// 	logger.Warn("Authentication disabled or not using k8s_token")
		// 	auditLogger.LogAuthenticationAttempt("anonymous", "access", map[string]string{"path": c.Request.URL.Path})
		// 	auditLogger.LogAuthenticationSuccess("anonymous", "access", map[string]string{"path": c.Request.URL.Path})
		// 	c.Next()
		// 	return
		// }

		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			logger.Warn("Missing or invalid Authorization header")
			auditLogger.LogAuthenticationFailure("anonymous", "access", "Missing Authorization header", map[string]string{"path": c.Request.URL.Path})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		tr := &authenticationv1.TokenReview{
			Spec: authenticationv1.TokenReviewSpec{Token: token},
		}
		result, err := clientset.AuthenticationV1().TokenReviews().Create(c.Request.Context(), tr, metav1.CreateOptions{})
		if err != nil || !result.Status.Authenticated {
			logger.Warn("Token validation failed", zap.Error(err))
			auditLogger.LogAuthenticationFailure("anonymous", "access", "Invalid token", map[string]string{"path": c.Request.URL.Path})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		// Store user info in context
		c.Set(string(UserKey), result.Status.User)
		logger.Info("User authenticated", zap.String("username", result.Status.User.Username))
		auditLogger.LogAuthenticationSuccess(result.Status.User.Username, "access", map[string]string{
			"path":     c.Request.URL.Path,
			"method":   c.Request.Method,
			"username": result.Status.User.Username,
		})
		c.Next()
	}
}

// RBAC checks permissions for requested actions
func RBAC(clientset *kubernetes.Clientset, logger *zap.Logger, auditLogger *logging.AuthLogger, permissions []ResourcePermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := c.Get(string(UserKey))
		if !exists {
			logger.Warn("No user info in context")
			auditLogger.LogAuthorizationCheck("anonymous", "access", "denied", map[string]string{
				"path":   c.Request.URL.Path,
				"error":  "No user info in context",
				"method": c.Request.Method,
			})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "No user info in context"})
			return
		}

		userInfo := user.(authenticationv1.UserInfo)
		for _, perm := range permissions {
			sar := &authorizationv1.SubjectAccessReview{
				Spec: authorizationv1.SubjectAccessReviewSpec{
					User:   userInfo.Username,
					Groups: userInfo.Groups,
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: perm.Namespace,
						Verb:      perm.Verb,
						Group:     perm.APIGroup,
						Resource:  perm.Resource,
					},
				},
			}
			result, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(c.Request.Context(), sar, metav1.CreateOptions{})
			if err != nil {
				logger.Error("SubjectAccessReview failed", zap.Error(err))
				auditLogger.LogAuthorizationCheck(userInfo.Username, "access", "error", map[string]string{
					"path":     c.Request.URL.Path,
					"method":   c.Request.Method,
					"resource": perm.Resource,
					"verb":     perm.Verb,
					"error":    err.Error(),
				})
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
				return
			}
			if !result.Status.Allowed {
				logger.Warn("Permission denied", zap.String("user", userInfo.Username), zap.String("resource", perm.Resource), zap.String("verb", perm.Verb))
				auditLogger.LogAuthorizationCheck(userInfo.Username, "access", "denied", map[string]string{
					"path":     c.Request.URL.Path,
					"method":   c.Request.Method,
					"resource": perm.Resource,
					"verb":     perm.Verb,
				})
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": fmt.Sprintf("User %s cannot %s %s in namespace %s", userInfo.Username, perm.Verb, perm.Resource, perm.Namespace),
				})
				return
			}
		}

		logger.Info("Permission granted", zap.String("user", userInfo.Username))
		auditLogger.LogAuthorizationCheck(userInfo.Username, "access", "allowed", map[string]string{
			"path":     c.Request.URL.Path,
			"method":   c.Request.Method,
			"username": userInfo.Username,
		})
		c.Next()
	}
}
