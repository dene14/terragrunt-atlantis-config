package cmd

import "errors"

// markerError wraps errors caused by malformed values of THIS tool's own
// locals annotations (extra_atlantis_dependencies etc). They stay fatal,
// while generic terragrunt/eval failures are tolerated per project (a huge
// monorepo must not stop generating because one leaf can't be evaluated
// in generation context).
type markerError struct{ err error }

func (e markerError) Error() string { return e.err.Error() }

// asMarkerError returns a fatalKind-wrapped error
func asMarkerError(err error) error { return markerError{err} }

// isMarkerError reports whether err is a fatal marker (our own annotations).
func isMarkerError(err error) bool {
	var m markerError
	return errors.As(err, &m)
}
