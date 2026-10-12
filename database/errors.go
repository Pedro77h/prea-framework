package database

import "errors"

// The driver's errors are translated into these; core.Error maps them to HTTP.
var (
	ErrNotFound     = errors.New("not found")     // no row; 404
	ErrConflict     = errors.New("conflict")      // unique violation; 409
	ErrInvalidInput = errors.New("invalid input") // malformed value, not null, foreign key, check; 400
)
