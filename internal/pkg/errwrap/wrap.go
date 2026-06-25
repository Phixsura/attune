// SPDX-License-Identifier: Apache-2.0

// Package errwrap provides utilities for consistent error wrapping.
package errwrap

import (
	"errors"
	"fmt"
)

// Wrap wraps an error with context if it's not nil.
// Returns nil if err is nil.
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf wraps an error with formatted context if it's not nil.
// Returns nil if err is nil.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}

// Is reports whether any error in err's chain matches target.
// Shortcut for errors.Is.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target.
// Shortcut for errors.As.
func As(err error, target any) bool {
	return errors.As(err, target)
}

// Join returns an error that wraps the given errors.
// Shortcut for errors.Join.
func Join(errs ...error) error {
	return errors.Join(errs...)
}

// New returns an error that formats as the given text.
// Shortcut for errors.New.
func New(text string) error {
	return errors.New(text)
}

// Newf returns an error that formats as the given format and args.
func Newf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// IfErr returns nil if err is nil, otherwise returns the wrapped error.
// Useful for one-liner error returns: return errwrap.IfErr(err, "context")
func IfErr(err error, msg string) error {
	return Wrap(err, msg)
}

// Must panics if err is not nil. Use only at init time or for
// programming errors that should never happen.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
