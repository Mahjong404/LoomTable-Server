package domain

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrNotFound          = errors.New("resource not found")
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrDependencyMissing = errors.New("application dependency is not configured")
)

type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string {
	if e.Message == "" {
		return "bad request"
	}
	return e.Message
}

type ValidationIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	return "request validation failed"
}

func NewValidationError(issues ...ValidationIssue) *ValidationError {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Path < issues[j].Path
	})
	return &ValidationError{Issues: issues}
}

type RevisionConflictError struct {
	Resource         string
	ID               string
	ExpectedRevision int64
	CurrentRevision  int64
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("%s %s revision is %d, expected %d", e.Resource, e.ID, e.CurrentRevision, e.ExpectedRevision)
}

type IdempotencyKeyReusedError struct{}

func (e *IdempotencyKeyReusedError) Error() string {
	return "idempotency key was used for a different request"
}

type ResourceLimitError struct {
	Resource   string
	ParentType string
	ParentID   string
	Limit      int
}

func (e *ResourceLimitError) Error() string {
	return fmt.Sprintf("%s limit %d reached", e.Resource, e.Limit)
}

type InvalidStateTransitionError struct {
	Resource string
	ID       string
	Action   string
	Current  string
}

func (e *InvalidStateTransitionError) Error() string {
	return fmt.Sprintf("cannot %s %s %s while it is %s", e.Action, e.Resource, e.ID, e.Current)
}
