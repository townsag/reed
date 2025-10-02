package service

type ErrorType int

const (
	ErrorNotFound ErrorType = iota
	ErrorRepoImpl 
	ErrorConflict
	ErrorInvalid
)

type DomainError struct {
	Type ErrorType
	Msg string
	Err error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func NotFound(msg string) *DomainError {
	return &DomainError{
		Type: ErrorNotFound,
		Msg: msg,
	}
}

func RepoImpl(err error) *DomainError {
	return &DomainError{
		Type: ErrorRepoImpl,
		Err: err,
	}
}