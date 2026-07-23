package common

import "errors"

var ErrTurnConflict = errors.New("turn state conflict")
var ErrNotFound = errors.New("record not found")
var ErrPendingDisconnected = errors.New("pending request disconnected")
var ErrConversationPending = errors.New("pending conversation cannot be deleted")
