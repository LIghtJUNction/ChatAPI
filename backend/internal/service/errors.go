package service

import "errors"

var ErrForbidden = errors.New("forbidden")
var ErrUserDeletionBlocked = errors.New("user deletion is blocked")
