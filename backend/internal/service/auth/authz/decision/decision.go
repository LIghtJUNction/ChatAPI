package decision

import "context"

type SubjectKind string

const (
	SubjectNone        SubjectKind = "none"
	SubjectSession     SubjectKind = "session"
	SubjectAppAPIKey   SubjectKind = "app_api_key"
	SubjectModelAPIKey SubjectKind = "model_api_key"
	SubjectAdminAPI    SubjectKind = "admin_api"
	SubjectCSRF        SubjectKind = "csrf"
)

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

type Reason string

const (
	ReasonNone         Reason = ""
	ReasonUnauthorized Reason = "unauthorized"
	ReasonForbidden    Reason = "forbidden"
	ReasonRateLimited  Reason = "rate_limited"
	ReasonSourceIP     Reason = "source_ip_forbidden"
	ReasonSessionCSRF  Reason = "csrf_origin_check_failed"
)

type Result struct {
	Effect     Effect      `json:"effect"`
	Subject    SubjectKind `json:"subject"`
	Reason     Reason      `json:"reason,omitempty"`
	StatusCode int         `json:"status_code,omitempty"`
	Message    string      `json:"message,omitempty"`
	ErrorCode  string      `json:"error_code,omitempty"`
}

func Allow(subject SubjectKind) Result {
	return Result{Effect: EffectAllow, Subject: subject}
}

func Deny(subject SubjectKind, reason Reason, statusCode int, message string, errorCode string) Result {
	return Result{
		Effect:     EffectDeny,
		Subject:    subject,
		Reason:     reason,
		StatusCode: statusCode,
		Message:    message,
		ErrorCode:  errorCode,
	}
}

func (r Result) Allowed() bool { return r.Effect == EffectAllow }
func (r Result) Denied() bool  { return r.Effect == EffectDeny }

type Binder func(context.Context) context.Context

type AuthResult[T any] struct {
	Decision Result
	Value    T
	Bind     Binder
}

func AllowWith[T any](subject SubjectKind, value T, bind Binder) AuthResult[T] {
	return AuthResult[T]{
		Decision: Allow(subject),
		Value:    value,
		Bind:     bind,
	}
}

func DenyWith[T any](result Result) AuthResult[T] {
	return AuthResult[T]{Decision: result}
}

func (r AuthResult[T]) BindContext(ctx context.Context) context.Context {
	if r.Bind == nil {
		return ctx
	}
	return r.Bind(ctx)
}
