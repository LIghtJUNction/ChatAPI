package common

import "errors"

var ErrTurnConflict = errors.New("turn state conflict")
var ErrNotFound = errors.New("record not found")
