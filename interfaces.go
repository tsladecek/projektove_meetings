package projektovemeeting

import (
	"context"
	"net/http"
)

type Projektove interface {
	CreateIssue(ctx context.Context, user User, obj ProjektoveIssueCreate) (ProjektoveIssue, error)
	GetProjects(ctx context.Context, user User) ([]ProjektoveProject, error)
}

type LLM interface {
	Infer(ctx context.Context, prompt string) (string, error)
}

type Repository interface {
	StorePrompt(ctx context.Context, user User, obj PromptCreate) (id int, uuid string, err error)
	ListPrompts(ctx context.Context, user User, limit, offset int) ([]Prompt, bool, error)
	GetPrompt(ctx context.Context, user User, id int) (Prompt, error)
	GetPromptByUUID(ctx context.Context, user User, uuid string) (Prompt, error)
	SetPromptProcessing(ctx context.Context, user User, id int, prompt string) error
	CompletePrompt(ctx context.Context, user User, id int, obj PromptComplete) error

	EnqueueTask(ctx context.Context, obj TaskCreate) (int, error)
	ClaimTask(ctx context.Context) (Task, bool, error)
	CompleteTask(ctx context.Context, id int, status TaskStatus, errMsg string) error
	ResetOrphanedTasks(ctx context.Context) error

	UpdateProjectsCache(ctx context.Context, user User, projects []ProjektoveProject) error
	ListProjects(ctx context.Context, user User) (ProjectsCacheEntry, error)

	GetContext(ctx context.Context, user User, id int) (LLMContext, error)
	GetContextByUUID(ctx context.Context, user User, uuid string) (LLMContext, error)
	ListContexts(ctx context.Context, user User) ([]LLMContext, error)
	StoreContext(ctx context.Context, user User, c LLMContextCreate) (id int, uuid string, err error)
	DeleteContextByUUID(ctx context.Context, user User, uuid string) error

	StoreIssue(ctx context.Context, user User, issue IssueCreate) (int, error)
	UpdateIssue(ctx context.Context, user User, parent IssueParent, parentID int, id int, obj IssueUpdate) error
	ListIssues(ctx context.Context, user User, parent IssueParent, parentID int) ([]Issue, error)
	GetIssue(ctx context.Context, user User, parent IssueParent, parentID int, id int) (Issue, error)
	GetIssueByUUID(ctx context.Context, user User, uuid string) (Issue, error)

	GetUser(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id int) (User, error)
	StoreUser(ctx context.Context, obj UserCreate) (int, error)
	UpdateUser(ctx context.Context, user User, obj UserUpdate) error
}

type Auth interface {
	Authenticate(ctx context.Context, token string) (User, error)
	RegisterRoutes(m *http.ServeMux)
	Middleware(next http.Handler) http.Handler
}
