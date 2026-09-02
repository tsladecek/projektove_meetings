package projektovemeeting

import (
	"context"
	"time"
)

type Projektove interface {
	CreateIssue(ctx context.Context, obj ProjektoveIssueCreate) error
	GetProjects(ctx context.Context) ([]ProjektoveProject, error)
}

type LLM interface {
	Infer(ctx context.Context, prompt string) (string, error)
}

type ProjectsCacheEntry struct {
	Projects  []ProjektoveProject
	FetchedAt time.Time
}

type DB interface {
	StorePrompt(ctx context.Context, prompt string, result string, error error) error
	UpdateProjectsCache(ctx context.Context, projects []ProjektoveProject) error
	ListProjects(ctx context.Context) (ProjectsCacheEntry, error)
}
