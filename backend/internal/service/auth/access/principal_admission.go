package access

import (
	"context"
	"net/http"
	"strings"
	"time"

	app "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	model "github.com/zyf/chatapi/internal/service/auth/authz/modelkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/session"
)

func (s *Service) AllowPrincipalRequest(ctx context.Context, r *http.Request) bool {
	return s.PrincipalAdmissionDecision(ctx).Result.Effect != "deny"
}

func newMultiLimiter() *multiLimiter {
	return &multiLimiter{
		now:     func() time.Time { return time.Now().UTC() },
		buckets: map[string][]time.Time{},
	}
}

func (l *multiLimiter) Allow(subjects []principalSubject, settings Settings) bool {
	_, ok := l.FirstDenied(subjects, settings)
	return !ok
}

func (l *multiLimiter) FirstDenied(subjects []principalSubject, settings Settings) (principalSubject, bool) {
	if l == nil {
		return principalSubject{}, false
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	normalized := normalizePrincipalSubjects(subjects, settings)
	for _, subject := range normalized {
		cutoff := now.Add(-subject.window)
		hits := l.buckets[subject.key]
		kept := hits[:0]
		for _, hit := range hits {
			if hit.After(cutoff) {
				kept = append(kept, hit)
			}
		}
		if len(kept) >= subject.max {
			l.buckets[subject.key] = kept
			return subject, true
		}
		l.buckets[subject.key] = kept
	}
	for _, subject := range normalized {
		l.buckets[subject.key] = append(l.buckets[subject.key], now)
	}
	return principalSubject{}, false
}

func (s *Service) currentSettings(ctx context.Context) Settings {
	if s == nil || s.settings == nil {
		return Settings{}
	}
	value, err := s.settings.Get(ctx)
	if err != nil {
		return s.settings.defaults
	}
	return value
}

func principalSubjectsFromContext(ctx context.Context) []principalSubject {
	items := make([]principalSubject, 0, 4)
	if pr, ok := session.PrincipalFromContext(ctx); ok {
		if strings.TrimSpace(pr.UserID) != "" {
			items = append(items, principalSubject{kind: "user", key: "user:" + strings.TrimSpace(pr.UserID)})
		}
		if strings.TrimSpace(pr.SubjectID) != "" {
			items = append(items, principalSubject{kind: "session", key: "session:" + strings.TrimSpace(pr.SubjectID)})
		}
	}
	if pr, ok := app.PrincipalFromContext(ctx); ok {
		if strings.TrimSpace(pr.UserID) != "" {
			items = append(items, principalSubject{kind: "user", key: "user:" + strings.TrimSpace(pr.UserID)})
		}
		if strings.TrimSpace(pr.KeyID) != "" {
			items = append(items, principalSubject{kind: "app_key", key: "app_key:" + strings.TrimSpace(pr.KeyID)})
		}
	}
	if pr, ok := model.PrincipalFromContext(ctx); ok {
		if strings.TrimSpace(pr.UserID) != "" {
			items = append(items, principalSubject{kind: "user", key: "user:" + strings.TrimSpace(pr.UserID)})
		}
		if strings.TrimSpace(pr.KeyID) != "" {
			items = append(items, principalSubject{kind: "model_key", key: "model_key:" + strings.TrimSpace(pr.KeyID)})
		}
	}
	return dedupePrincipalSubjects(items)
}

func dedupePrincipalSubjects(items []principalSubject) []principalSubject {
	seen := map[string]struct{}{}
	out := make([]principalSubject, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.key) == "" {
			continue
		}
		if _, ok := seen[item.key]; ok {
			continue
		}
		seen[item.key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizePrincipalSubjects(items []principalSubject, settings Settings) []principalSubject {
	out := make([]principalSubject, 0, len(items))
	for _, item := range items {
		switch item.kind {
		case "user":
			item.max = settings.UserRateLimitRequests
			item.window = settings.UserRateLimitWindow
		case "session":
			item.max = settings.SessionRateLimitRequests
			item.window = settings.SessionRateLimitWindow
		case "app_key":
			item.max = settings.AppKeyRateLimitRequests
			item.window = settings.AppKeyRateLimitWindow
		case "model_key":
			item.max = settings.ModelKeyRateLimitRequests
			item.window = settings.ModelKeyRateLimitWindow
		}
		if item.max > 0 && item.window > 0 {
			out = append(out, item)
		}
	}
	return out
}
