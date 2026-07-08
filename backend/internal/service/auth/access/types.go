package access

import (
	"sync"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
)

type LabDecisionKind string

const (
	LabDecisionAllow  LabDecisionKind = "allow"
	LabDecisionGrant  LabDecisionKind = "grant"
	LabDecisionRender LabDecisionKind = "render"
	LabDecisionDeny   LabDecisionKind = "deny"
)

type LabDecision struct {
	Kind       LabDecisionKind
	RedirectTo string
}

type Service struct {
	cfg              config.Config
	lab              *labauth.Service
	settings         *SettingsService
	trustedOrigins   map[string]struct{}
	anonymousLimiter *requestLimiter
	principalLimiter *multiLimiter
}

type requestLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	max      int
	window   time.Duration
	requests map[string][]time.Time
}

type principalSubject struct {
	kind   string
	key    string
	max    int
	window time.Duration
}

type multiLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[string][]time.Time
}
