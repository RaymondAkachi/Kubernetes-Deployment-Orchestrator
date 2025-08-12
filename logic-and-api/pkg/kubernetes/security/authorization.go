package security

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ResourcePermission struct {
	APIGroup  string
	Resource  string
	Verb      string
	Namespace string
}

// RBAC checks if the user has permission for the requested action
func RBAC(clientset *kubernetes.Clientset, logger *zap.Logger, permissions []ResourcePermission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(UserKey).(authenticationv1.UserInfo)
			if !ok {
				logger.Warn("No user info in context")
				http.Error(w, "Unauthorized: No user info", http.StatusUnauthorized)
				return
			}

			for _, perm := range permissions {
				sar := &authorizationv1.SubjectAccessReview{
					Spec: authorizationv1.SubjectAccessReviewSpec{
						User:   user.Username,
						Groups: user.Groups,
						ResourceAttributes: &authorizationv1.ResourceAttributes{
							Namespace: perm.Namespace,
							Verb:      perm.Verb,
							Group:     perm.APIGroup,
							Resource:  perm.Resource,
						},
					},
				}
				result, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(r.Context(), sar, metav1.CreateOptions{})
				if err != nil {
					logger.Error("SubjectAccessReview failed", zap.Error(err))
					http.Error(w, "Internal server error", http.StatusInternalServerError)
					return
				}
				if !result.Status.Allowed {
					logger.Warn("Permission denied", zap.String("user", user.Username), zap.String("resource", perm.Resource), zap.String("verb", perm.Verb))
					http.Error(w, fmt.Sprintf("Forbidden: User %s cannot %s %s in namespace %s", user.Username, perm.Verb, perm.Resource, perm.Namespace), http.StatusForbidden)
					return
				}
			}

			logger.Info("Permission granted", zap.String("user", user.Username))
			next.ServeHTTP(w, r)
		})
	}
}
