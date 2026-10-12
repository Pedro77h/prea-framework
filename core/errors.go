package core

import "net/http"

// HTTPError is an error with the status Error replies with. Use the net/http
// constants: core.NewError(http.StatusForbidden, "not the owner").
type HTTPError struct {
	Status  int
	Message string
}

func NewError(status int, message string) error {
	return &HTTPError{status, message}
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return http.StatusText(e.Status)
	}
	return e.Message
}
