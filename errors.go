package van

import "fmt"

var (
	errInvalidType      = newError("invalid type")
	errProviderNotFound = newError("provider not found")
)

type localError struct {
	parent error
	msg    string
}

func newError(msg string) *localError {
	return &localError{msg: msg}
}

func (err *localError) new(msg string) *localError {
	return &localError{
		parent: err,
		msg:    msg,
	}
}

func (err *localError) fmt(format string, a ...interface{}) *localError {
	return err.new(fmt.Sprintf(format, a...))
}

func (err *localError) Error() string {
	return err.msg
}

func (err *localError) Unwrap() error {
	return err.parent
}
