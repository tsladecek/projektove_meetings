package projektovemeeting

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNoProjectsFound              error = errors.New("no projects found")
	ErrUserNotFound                       = errors.New("user not found")
	ErrContextNotFound                    = errors.New("context not found")
	ErrContextInUse                       = errors.New("context is in use")
	ErrIssueNotFound                      = errors.New("issue not found")
	ErrPromptNotFound                     = errors.New("prompt not found")
	ErrBatchNotFound                      = errors.New("batch not found")
	ErrBatchCSVInvalid                    = errors.New("batch csv is invalid")
	ErrIssueSubmitted                     = errors.New("issue already submitted")
	ErrIssueIgnored                       = errors.New("issue is ignored")
	ErrModelNotFound                      = errors.New("model not found")
	ErrParentDoesNotBelongToUser          = errors.New("parent does not belong to user")
	ErrIssueIncomplete                    = errors.New("issue is missing required fields")
	ErrProjektoveTokenNotConfigured       = errors.New("projektove token is not configured")
	ErrInvalidCredentials                 = errors.New("invalid credentials")
)

// BatchCSVError carries the list of human-readable validation problems found
// while parsing an uploaded batch CSV.
type BatchCSVError struct {
	Messages []string
}

func (e *BatchCSVError) Error() string {
	return fmt.Sprintf("%v:\n%s", ErrBatchCSVInvalid, strings.Join(e.Messages, "\n"))
}

func (e *BatchCSVError) Unwrap() error {
	return ErrBatchCSVInvalid
}
