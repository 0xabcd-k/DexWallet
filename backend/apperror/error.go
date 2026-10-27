package apperror

import "fmt"

// Error carries a stable public code and an internal diagnostic cause.
type Error struct {
	Code  Code
	Cause error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return string(e.Code)
}
func (e *Error) Unwrap() error         { return e.Cause }
func New(code Code, cause error) error { return &Error{Code: code, Cause: cause} }
