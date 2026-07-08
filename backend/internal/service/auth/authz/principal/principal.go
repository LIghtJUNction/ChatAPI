package principal

import (
	"strings"

	"github.com/zyf/chatapi/internal/actor"
)

type Kind string

const (
	KindHumanSession Kind = "human_session"
	KindAppAPIKey    Kind = "app_api_key"
	KindModelAPIKey  Kind = "model_api_key"
)

type Principal struct {
	Kind       Kind
	SubjectID  string
	UserID     string
	Username   string
	Role       string
	IsAdmin    bool
	Source     string
	EntryPoint string
	AuthMethod string
}

func (p Principal) Valid() bool {
	return strings.TrimSpace(p.SubjectID) != "" && strings.TrimSpace(p.UserID) != ""
}

func (p Principal) Actor() actor.Actor {
	role := strings.TrimSpace(p.Role)
	if role == "" {
		role = "user"
	}
	return actor.Actor{
		UserID:      strings.TrimSpace(p.UserID),
		Username:    strings.TrimSpace(p.Username),
		Role:        role,
		Source:      strings.TrimSpace(p.Source),
		PrincipalID: strings.TrimSpace(p.SubjectID),
		EntryPoint:  strings.TrimSpace(p.EntryPoint),
	}
}

func (p Principal) CanAccessUser(userID string) bool {
	if strings.TrimSpace(userID) == "" {
		return false
	}
	return strings.TrimSpace(p.UserID) == strings.TrimSpace(userID)
}
