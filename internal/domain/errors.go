package domain

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownLanguage   = errors.New("unknown language")
	ErrNotInstalled      = errors.New("language is not installed")
	ErrVersionNotFound   = errors.New("version not found")
	ErrAmbiguousVersion  = errors.New("version matches several installations")
	ErrActiveVersion     = errors.New("version is active")
	ErrInvalidVarName    = errors.New("invalid environment variable name")
	ErrForeignVar        = errors.New("environment variable does not belong to the language")
	ErrInvalidTheme      = errors.New("invalid theme")
	ErrRemovalNotAllowed = errors.New("version cannot be removed by lman")
)

// ManualRemovalError means the version has to be removed by the user, usually with administrator rights.
type ManualRemovalError struct {
	Hint string
}

func (e *ManualRemovalError) Error() string {
	if e.Hint == "" {
		return ErrRemovalNotAllowed.Error()
	}
	return fmt.Sprintf("%s: run %s", ErrRemovalNotAllowed, e.Hint)
}

func (e *ManualRemovalError) Unwrap() error {
	return ErrRemovalNotAllowed
}
