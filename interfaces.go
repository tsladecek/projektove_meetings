package projektovemeeting

import "context"

type Projektove interface {
	CreateIssue(ctx context.Context, obj ProjektoveIssueCreate) error
	GetProjects(ctx context.Context) ([]ProjektoveProject, error)
}

type LLM interface {
	Infer(ctx context.Context, prompt string) (string, error)
}
