package policy

import "strings"

type Service struct{ superAdminEmail string }

func NewService(superAdminEmail ...string) *Service {
	email := ""
	if len(superAdminEmail) > 0 {
		email = strings.ToLower(strings.TrimSpace(superAdminEmail[0]))
	}
	return &Service{superAdminEmail: email}
}
