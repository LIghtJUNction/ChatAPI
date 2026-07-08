package middleware

import (
	"net/http"

	app "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/decision"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
)

func RequireUserSessionOrAppAPI(policyService *policy.Service, appAuth func(http.Handler) http.Handler, requireSession func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, hasSession := session.PrincipalFromContext(r.Context())
			hasApp := app.ExtractRequestAPIKey(r) != ""
			switch result := policyService.UserOrAppDecision(hasSession, hasApp); result.Subject {
			case decision.SubjectSession:
				requireSession(next).ServeHTTP(w, r)
			case decision.SubjectAppAPIKey:
				appAuth(next).ServeHTTP(w, r)
			default:
				WriteDecisionError(w, result)
			}
		})
	}
}
