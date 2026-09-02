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

type LLMContext struct {
	ID      int // not specified when creating
	Context string
	Name    string
}

type LLMContextCreate struct {
	Context string
	Name    string
}

type PromptCreate struct {
	Prompt    string
	Result    string
	Error     error
	ContextID int
}

type Prompt struct {
	ID      string // not specified when creating
	Prompt  string
	Result  string
	Error   error
	Context LLMContext
}

type IssueCreate struct {
	PromptID     int
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AuthorID     int
	AssignedToID int
}
type Issue struct {
	ID           int
	PromptID     int
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AuthorID     int
	AssignedToID int
}

type IssueStatus string

const (
	IssueStatusCreated      IssueStatus = "created"
	IssueStatusSubmitted    IssueStatus = "submitted"
	IssueStatusSubmitFailed IssueStatus = "submit_failed"
)

type DB interface {
	StorePrompt(ctx context.Context, obj PromptCreate) error
	ListPrompts(ctx context.Context) ([]Prompt, error)

	UpdateProjectsCache(ctx context.Context, projects []ProjektoveProject) error
	ListProjects(ctx context.Context) (ProjectsCacheEntry, error)

	GetContext(ctx context.Context, id int) (LLMContext, error)
	ListContexts(ctx context.Context) ([]LLMContext, error)
	StoreContext(ctx context.Context, c LLMContextCreate) error

	StoreIssue(ctx context.Context, issue IssueCreate) error
	UpdateIssueStatus(ctx context.Context, id int, status IssueStatus) error
	ListIssues(ctx context.Context, promptID int) ([]Issue, error)
}
