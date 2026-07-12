package model

import "github.com/zyf2007/ChatAPI/internal/actor"

type Principal struct {
	KeyID     string
	UserID    string
	Name      string
	KeyPrefix string
	Model     string
}

type AdmissionInput struct {
	RawKey string
}

type AdmissionValue struct {
	Principal Principal
	Actor     actor.Actor
}
