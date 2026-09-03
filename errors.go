package projektovemeeting

import "errors"

var (
	ErrNoProjectsFound           error = errors.New("no projects found")
	ErrUserNotFound                    = errors.New("user not found")
	ErrContextNotFound                 = errors.New("context not found")
	ErrIssueNotFound                   = errors.New("issue not found")
	ErrParentDoesNotBelongToUser       = errors.New("parent does not belong to user")
)
