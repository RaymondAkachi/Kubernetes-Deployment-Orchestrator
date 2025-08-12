package security

import (
	"context"
	"net/http"
	"strings"

	"go.uber.org/zap"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

type contextKey string

const UserKey contextKey = "user"

// Auth validates Kubernetes tokens using TokenReview
func Auth(clientset *kubernetes.Clientset, cfg *config.Config, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Security.Auth.Enabled || cfg.Security.Auth.Type != "k8s_token" {
				logger.Warn("Authentication is disabled or not using k8s_token")
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				logger.Warn("Missing or invalid Authorization header")
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			tr := &authenticationv1.TokenReview{
				Spec: authenticationv1.TokenReviewSpec{Token: token},
			}
			result, err := clientset.AuthenticationV1().TokenReviews().Create(r.Context(), tr, metav1.CreateOptions{})
			if err != nil || !result.Status.Authenticated {
				logger.Warn("Token validation failed", zap.Error(err))
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Store user identity in context
			ctx := context.WithValue(r.Context(), UserKey, result.Status.User)
			logger.Info("User authenticated", zap.String("username", result.Status.User.Username))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
