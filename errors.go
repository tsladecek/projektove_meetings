package projektovemeeting

import "errors"

var (
	ErrNoProjectsFound error = errors.New("no projects found")
	ErrUserNotFound          = errors.New("user not found")
)
