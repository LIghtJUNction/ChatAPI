package app

import "github.com/zyf2007/ChatAPI/internal/actor"

type Principal struct {
	KeyID                string
	UserID               string
	Name                 string
	KeyPrefix            string
	Scopes               map[string]struct{}
	ResourceLimits       map[string]any
	AllowedActions       map[string]struct{}
	MaxRequestsPerMinute int
	AllowedSourceIPs     []string
}

type AdmissionInput struct {
	RawKey         string
	SourceIP       string
	Route          string
	RequiredScopes []string
}

type AdmissionValue struct {
	Principal Principal
	Actor     actor.Actor
}
