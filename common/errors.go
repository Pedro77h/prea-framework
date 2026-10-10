package common

import "errors"

// Domain errors shared by every layer. Wrap them with fmt.Errorf("%w: ...")
// and core.Error maps them to the right HTTP status.
var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrConflict     = errors.New("conflict")
)
