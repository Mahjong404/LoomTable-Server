package status

import "errors"

var (
	ErrDependencyUnavailable = errors.New("dependency unavailable")
	ErrMigrationRequired     = errors.New("migration required")
)
