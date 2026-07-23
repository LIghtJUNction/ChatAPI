package profile_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	userprofile "github.com/zyf2007/ChatAPI/internal/service/usercontrol/profile"
)

type fakeIdentity struct {
	user common.User
	err  error
}

func (f fakeIdentity) GetUser(context.Context, string) (common.User, error) {
	if f.err != nil {
		return common.User{}, f.err
	}
	return f.user, nil
}

type fakeSettings struct {
	value authsettings.PublicSettings
	err   error
}

func (f fakeSettings) Public(context.Context) (authsettings.PublicSettings, error) {
	return f.value, f.err
}

type fakeTOTP struct {
	enabled bool
}
type fakeRealtime struct{ value workspacesettings.Settings }

func (f fakeRealtime) Current(context.Context) (workspacesettings.Settings, error) {
	return f.value, nil
}

type fakeConversations struct{ items []common.Conversation }

func (f fakeConversations) ListConversationsForOwner(context.Context, string) ([]common.Conversation, error) {
	return f.items, nil
}

func (f fakeTOTP) IsEnabled(context.Context, string) bool { return f.enabled }

type fakeLocalAuth struct {
	lastUserID string
	lastPass   string
	err        error
}

func (f *fakeLocalAuth) UpdatePasswordForUser(_ context.Context, userID string, password string) error {
	f.lastUserID = userID
	f.lastPass = password
	return f.err
}

func TestProfileServiceAnonymousFallbackSettings(t *testing.T) {
	svc := userprofile.New(userprofile.Deps{})
	view, err := svc.BuildAnonymousSessionView(context.Background(), config.Config{
		GeetestCaptchaID:              "captcha-id",
		OIDCEnabled:                   true,
		RealtimeMaxConnectionsPerUser: 7,
	})
	if err != nil {
		t.Fatalf("build anonymous session view: %v", err)
	}
	if view.Authenticated || !view.GeeTestEnabled || view.GeeTestCaptchaID != "captcha-id" || view.RealtimeMaxConnectionsPerUser != 7 {
		t.Fatalf("unexpected anonymous view: %#v", view)
	}
}

func TestProfileServicePublicSettingsFallback(t *testing.T) {
	svc := userprofile.New(userprofile.Deps{})
	settings, err := svc.PublicSettings(context.Background(), config.Config{OIDCEnabled: true})
	if err != nil {
		t.Fatalf("public settings: %v", err)
	}
	if !settings.LocalPasswordLoginEnabled || !settings.RegistrationEnabled || !settings.OIDCEnabled {
		t.Fatalf("unexpected public settings: %#v", settings)
	}
}

func TestProfileErrAlias(t *testing.T) {
	if userprofile.ErrNewPasswordRequired == nil {
		t.Fatal("expected ErrNewPasswordRequired alias")
	}
	_ = authsettings.PublicSettings{}
}

func TestProfileServiceBuildAuthenticatedSessionViewWithRoleAndTOTP(t *testing.T) {
	svc := userprofile.New(userprofile.Deps{
		Identity: fakeIdentity{user: common.User{
			ID:         "user_a",
			Username:   "alice",
			Role:       "",
			LocalAdmin: true,
		}},
		Settings: fakeSettings{value: authsettings.PublicSettings{
			LocalPasswordLoginEnabled: true,
			RegistrationEnabled:       false,
			EmailVerificationEnabled:  true,
			OIDCEnabled:               true,
			OIDCProviderName:          "Kirari",
		}},
		TOTP:              fakeTOTP{enabled: true},
		Realtime:          fakeRealtime{value: workspacesettings.Settings{MaxConnectionsPerUser: 4}},
		Conversations:     fakeConversations{items: []common.Conversation{{ID: "conv_1"}, {ID: "conv_2"}}},
		ConversationLimit: func(context.Context) int { return 30 },
	})

	view, err := svc.BuildAuthenticatedSessionView(context.Background(), config.Config{
		RealtimeMaxConnectionsPerUser: 9,
	}, "user_a")
	if err != nil {
		t.Fatalf("build authenticated session view: %v", err)
	}
	if !view.Authenticated || !view.TOTPEnabled {
		t.Fatalf("unexpected auth/totp state: %#v", view)
	}
	if view.User["id"] != "user_a" || view.User["username"] != "alice" || view.User["role"] != "superadmin" {
		t.Fatalf("unexpected user payload: %#v", view.User)
	}
	if !view.OIDCEnabled || view.OIDCProviderName != "Kirari" || view.RealtimeMaxConnectionsPerUser != 4 || view.CurrentConversationCount != 2 || view.UserConversationLimit != 30 {
		t.Fatalf("unexpected settings payload: %#v", view)
	}
}

func TestProfileServiceBuildAuthenticatedSessionViewPropagatesErrors(t *testing.T) {
	want := errors.New("boom")
	svc := userprofile.New(userprofile.Deps{
		Identity: fakeIdentity{err: want},
		Settings: fakeSettings{value: authsettings.PublicSettings{LocalPasswordLoginEnabled: true}},
	})
	if _, err := svc.BuildAuthenticatedSessionView(context.Background(), config.Config{}, "user_a"); !errors.Is(err, want) {
		t.Fatalf("expected user lookup error, got %v", err)
	}

	svc = userprofile.New(userprofile.Deps{
		Identity: fakeIdentity{user: common.User{ID: "user_a"}},
		Settings: fakeSettings{err: want},
	})
	if _, err := svc.BuildAuthenticatedSessionView(context.Background(), config.Config{}, "user_a"); !errors.Is(err, want) {
		t.Fatalf("expected settings error, got %v", err)
	}
}

func TestProfileServiceChangePassword(t *testing.T) {
	local := &fakeLocalAuth{}
	svc := userprofile.New(userprofile.Deps{LocalAuth: local})
	if err := svc.ChangePassword(context.Background(), "user_a", "secret"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if local.lastUserID != "user_a" || local.lastPass != "secret" {
		t.Fatalf("unexpected local auth input: user=%q pass=%q", local.lastUserID, local.lastPass)
	}
}

func TestProfileServiceChangePasswordFailure(t *testing.T) {
	want := errors.New("update failed")
	local := &fakeLocalAuth{err: want}
	svc := userprofile.New(userprofile.Deps{LocalAuth: local})
	if err := svc.ChangePassword(context.Background(), "user_a", "secret"); !errors.Is(err, want) {
		t.Fatalf("expected change password error, got %v", err)
	}
}
