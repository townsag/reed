package service

import "fmt"

type DomainError interface {
	error
	isDomainError()
}

type RepoImplError struct {
	Msg string
	Err error
}
func (e *RepoImplError) Error() string {
	return fmt.Sprintf("repository implementation error, msg: %s, err: %v", e.Msg, e.Err)
}
func (e *RepoImplError) Unwrap() error { return e.Err }
func (e *RepoImplError) isDomainError() {}

type NotFoundError struct {
	Msg string
	Err error
}
func (e *NotFoundError) Error() string {
	return fmt.Sprintf("this resource was not found, msg: %s, err: %v", e.Msg, e.Err)
}
func (e *NotFoundError) Unwrap() error { return e.Err }
func (e *NotFoundError) isDomainError() {}

type InvalidInputError struct {
	Msg string
	Err error
}
func (e *InvalidInputError) Error() string {
	return fmt.Sprintf("received an invalid input: %s, err: %v", e.Msg, e.Err)
}
func (e *InvalidInputError) Unwrap() error { return e.Err }
func (e *InvalidInputError) isDomainError() {}

type UniqueConflictError struct {
	Msg string
	Err error
}

func (e *UniqueConflictError) Error() string {
	return fmt.Sprintf("%s, %v", e.Msg, e.Err)
}
func (e *UniqueConflictError) Unwrap() error { return e.Err }
func (e *UniqueConflictError) isDomainError() {}

func RepoImpl(msg string, err error) *RepoImplError {
	return &RepoImplError{
		Msg: msg,
		Err: err,
	}
}

func NotFound(msg string, err error) *NotFoundError {
	return &NotFoundError{
		Msg: msg,
		Err: err,
	}
}

func InvalidInput(msg string, err error) *InvalidInputError {
	return &InvalidInputError{
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

var ErrNilPointer error = fmt.Errorf("pointer must not be nil")