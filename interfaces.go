package projektovemeeting

import (
	"context"
	"time"
)

type Projektove interface {
	CreateIssue(ctx context.Context, obj ProjektoveIssueCreate) (ProjektoveIssue, error)
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

type IssueParent string

const (
	IssueParentPrompt IssueParent = "prompt"
	IssueParentBatch  IssueParent = "batch"
)

type IssueCreate struct {
	Parent       IssueParent
	ParentID     int
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AssignedToID int
}

type IssueUpdate struct {
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AssignedToID int
	Status       IssueStatus
	ProjektoveID *int
}

type Issue struct {
	ID           int
	Parent       IssueParent
	ParentID     int
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AssignedToID int
	ProjektoveID *int
	Status       IssueStatus
}

type UserCreate struct {
	Email           string
	ProjektoveToken string
	Models          []LLMModel
}

type UserUpdate struct {
	ProjektoveToken string
	Models          []LLMModel
}

type IssueStatus string

const (
	IssueStatusCreated      IssueStatus = "created"
	IssueStatusSubmitted    IssueStatus = "submitted"
	IssueStatusSubmitFailed IssueStatus = "submit_failed"
)

type Repository interface {
	StorePrompt(ctx context.Context, user User, obj PromptCreate) (int, error)
	ListPrompts(ctx context.Context, user User) ([]Prompt, error)

	UpdateProjectsCache(ctx context.Context, user User, projects []ProjektoveProject) error
	ListProjects(ctx context.Context, user User) (ProjectsCacheEntry, error)

	GetContext(ctx context.Context, user User, id int) (LLMContext, error)
	ListContexts(ctx context.Context, user User) ([]LLMContext, error)
	StoreContext(ctx context.Context, user User, c LLMContextCreate) (int, error)

	StoreIssue(ctx context.Context, user User, issue IssueCreate) (int, error)
	UpdateIssue(ctx context.Context, user User, id int, obj IssueUpdate) error
	ListIssues(ctx context.Context, user User, promptID int) ([]Issue, error)

	GetUser(ctx context.Context, email string) (User, error)
	StoreUser(ctx context.Context, obj UserCreate) (int, error)
	UpdateUser(ctx context.Context, user User, obj UserUpdate) error
}
