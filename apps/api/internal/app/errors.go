package app

import (
	"errors"
	"fmt"
)

// ErrorKind classifies startup failures so callers can map them to logs and exit codes.
type ErrorKind string

const (
	// KindConfig is invalid or missing configuration.
	KindConfig ErrorKind = "config"
	// KindDependency is a required shared dependency (Redis, durable audit, identity).
	KindDependency ErrorKind = "dependency"
	// KindProvider is a required managed-provider initialization failure.
	KindProvider ErrorKind = "provider"
	// KindInit is a general construction failure (policy, router, etc.).
	KindInit ErrorKind = "init"
)

// Error is a typed application startup error. Build never calls os.Exit.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func configErr(msg string, err error) error {
	return &Error{Kind: KindConfig, Message: msg, Err: err}
}

func dependencyErr(msg string, err error) error {
	return &Error{Kind: KindDependency, Message: msg, Err: err}
}

func providerErr(msg string, err error) error {
	return &Error{Kind: KindProvider, Message: msg, Err: err}
}

func initErr(msg string, err error) error {
	return &Error{Kind: KindInit, Message: msg, Err: err}
}

// IsKind reports whether err is an *Error of the given kind.
func IsKind(err error, kind ErrorKind) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Kind == kind
}
