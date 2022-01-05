package van

var (
	ErrInvalidType      = newError("invalid type")
	ErrProviderNotFound = newError("provider not found")
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

func (err *localError) Error() string {
	return err.msg
}

func (err *localError) Unwrap() error {
	return err.parent
}
