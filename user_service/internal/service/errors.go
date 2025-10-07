package service

import "fmt"

// could use this interface with empty isDomainError method to distinguish between errors that 
// fall under the domain error umbrella, not necessary
type DomainError interface {
	error
	isDomainError()	
}

type NotFoundError struct {
	Msg string
}

func (e *NotFoundError) Error() string {
	return e.Msg
}

func (e *NotFoundError) isDomainError() {}

type RepoImplError struct {
	Msg string
	Err error
}

func (e *RepoImplError) Error() string {
	return fmt.Sprintf("repository implementation error, msg: %s, err: %v", e.Msg, e.Err)
}

func (e *RepoImplError) Unwrap() error {
	return e.Err
}

func (e *RepoImplError) isDomainError() {}

type UniqueConflictError struct {
	Msg string
	Err error
}

func (e *UniqueConflictError) Error() string {
	return fmt.Sprintf("unique conflict, msg: %s, err: %v", e.Msg, e.Err)
}

func (e *UniqueConflictError) Unwrap() error {
	return e.Err
}

func (e *UniqueConflictError) isDomainError() {}

type InvalidError struct {
	Msg string
	Err error
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid input, msg: %s, err: %v", e.Msg, e.Err)
}

func (e *InvalidError) Unwrap() error {
	return e.Err
}

func (e *InvalidError) isDomainError() {}

type PasswordMismatchError struct {
	Err error
}

func (e *PasswordMismatchError) Error() string {
	return e.Err.Error()
}

func (e *PasswordMismatchError) isDomainError() {}

func NotFound(msg string) *NotFoundError {
	return &NotFoundError{
		Msg: msg,
	}
}

func RepoImpl(msg string, err error) *RepoImplError {
	return &RepoImplError{
		Msg: msg,
		Err: err,
	}
}

func UniqueConflict(msg string, err error) *UniqueConflictError {
	return &UniqueConflictError{
		Msg: msg,
		Err: err,
	}
}

func Invalid(msg string, err error) *InvalidError {
	return &InvalidError{
		Msg: msg,
		Err: err,
	}
}

func PasswordMismatch(err error) *PasswordMismatchError {
	return &PasswordMismatchError{
		Err: err,
	}
}