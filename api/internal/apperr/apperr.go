package apperr

import "errors"

var (
	ErrInvalidArgument = errors.New("invalid_argument")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not_found")
	ErrConflict        = errors.New("conflict")
	ErrRateLimited     = errors.New("rate_limited")
	ErrInvalidCode     = errors.New("invalid_code")
	ErrDisabled        = errors.New("disabled")
	ErrInvalidPassword = errors.New("invalid_password")
	ErrPhoneTaken      = errors.New("phone_taken")
	ErrEmailTaken      = errors.New("email_taken")
)

type Coded struct {
	Err  error
	Code string
	Msg  string
}

func (e Coded) Error() string { return e.Msg }
func (e Coded) Unwrap() error { return e.Err }

func New(err error, code, msg string) error {
	return Coded{Err: err, Code: code, Msg: msg}
}

func CodeOf(err error) (string, string, bool) {
	var c Coded
	if errors.As(err, &c) {
		return c.Code, c.Msg, true
	}
	return "", "", false
}
