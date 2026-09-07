package projektovemeeting

import "errors"

var (
	ErrNoProjectsFound           error = errors.New("no projects found")
	ErrUserNotFound                    = errors.New("user not found")
	ErrContextNotFound                 = errors.New("context not found")
	ErrContextInUse                    = errors.New("context is in use")
	ErrIssueNotFound                   = errors.New("issue not found")
	ErrPromptNotFound                  = errors.New("prompt not found")
	ErrIssueSubmitted                  = errors.New("issue already submitted")
	ErrModelNotFound                   = errors.New("model not found")
	ErrParentDoesNotBelongToUser       = errors.New("parent does not belong to user")
	ErrIssueIncomplete                 = errors.New("issue is missing required fields")
)
