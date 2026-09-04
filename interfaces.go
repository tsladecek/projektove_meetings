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
	StorePrompt(ctx context.Context, user User, obj PromptCreate) (int, error)
	ListPrompts(ctx context.Context, user User) ([]Prompt, error)

	UpdateProjectsCache(ctx context.Context, user User, projects []ProjektoveProject) error
	ListProjects(ctx context.Context, user User) (ProjectsCacheEntry, error)

	GetContext(ctx context.Context, user User, id int) (LLMContext, error)
	ListContexts(ctx context.Context, user User) ([]LLMContext, error)
	StoreContext(ctx context.Context, user User, c LLMContextCreate) (int, error)

	StoreIssue(ctx context.Context, user User, issue IssueCreate) (int, error)
	UpdateIssue(ctx context.Context, user User, parent IssueParent, parentID int, id int, obj IssueUpdate) error
	ListIssues(ctx context.Context, user User, parent IssueParent, parentID int) ([]Issue, error)

	GetUser(ctx context.Context, email string) (User, error)
	StoreUser(ctx context.Context, obj UserCreate) (int, error)
	UpdateUser(ctx context.Context, user User, obj UserUpdate) error
}

type Auth interface {
	Authenticate(ctx context.Context, token string) (User, error)
	RegisterRoutes(m *http.ServeMux)
	Middleware(next http.Handler) http.Handler
}
