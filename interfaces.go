package projektovemeeting

import (
	"context"
	"net/http"
)

type Projektove interface {
	CreateIssue(ctx context.Context, token, baseURL string, obj ProjektoveIssueCreate) (ProjektoveIssue, error)
	GetProjects(ctx context.Context, token, baseURL string) ([]ProjektoveProject, error)
}

type LLM interface {
	Infer(ctx context.Context, prompt string) (string, error)
}

type Repository interface {
	StorePrompt(ctx context.Context, org UserProjektoveOrganization, obj PromptCreate) (id int, uuid string, err error)
	ListPrompts(ctx context.Context, user User, limit, offset int) ([]Prompt, bool, error)
	GetPrompt(ctx context.Context, org UserProjektoveOrganization, id int) (Prompt, error)
	GetPromptByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Prompt, error)
	SetPromptProcessing(ctx context.Context, org UserProjektoveOrganization, id int, prompt string) error
	CompletePrompt(ctx context.Context, org UserProjektoveOrganization, id int, obj PromptComplete) error

	StoreBatch(ctx context.Context, org UserProjektoveOrganization, fileContent string) (id int, uuid string, err error)
	ListBatches(ctx context.Context, user User, limit, offset int) ([]Batch, bool, error)
	GetBatch(ctx context.Context, org UserProjektoveOrganization, id int) (Batch, error)
	GetBatchByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Batch, error)

	EnqueueTask(ctx context.Context, obj TaskCreate) (int, error)
	ClaimTask(ctx context.Context) (Task, bool, error)
	CompleteTask(ctx context.Context, id int, status TaskStatus, errMsg string) error
	ResetOrphanedTasks(ctx context.Context) error

	UpdateProjectsCache(ctx context.Context, org UserProjektoveOrganization, projects []ProjektoveProject) error
	ListProjects(ctx context.Context, org UserProjektoveOrganization) (ProjectsCacheEntry, error)

	GetContext(ctx context.Context, user User, id int) (LLMContext, error)
	GetContextByUUID(ctx context.Context, user User, uuid string) (LLMContext, error)
	ListContexts(ctx context.Context, user User) ([]LLMContext, error)
	StoreContext(ctx context.Context, user User, c LLMContextCreate) (id int, uuid string, err error)
	DeleteContextByUUID(ctx context.Context, user User, uuid string) error

	StoreIssue(ctx context.Context, org UserProjektoveOrganization, issue IssueCreate) (int, error)
	UpdateIssue(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int, id int, obj IssueUpdate) error
	ListIssues(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int) ([]Issue, error)
	GetIssue(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int, id int) (Issue, error)
	GetIssueByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Issue, error)

	GetUser(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id int) (User, error)
	StoreUser(ctx context.Context, obj UserCreate) (int, error)

	ListProviders(ctx context.Context) ([]Provider, error)
	GetOrCreateProvider(ctx context.Context, provider string) (int, error)
	GetOrCreateModel(ctx context.Context, providerID int, model string) (int, string, error)
	ListModels(ctx context.Context) ([]Model, error)
	ListUserModels(ctx context.Context, userID int) ([]UserModel, error)
	GetUserModelByUUID(ctx context.Context, userID int, uuid string) (UserModel, error)
	GetUserModelByModelID(ctx context.Context, userID int, modelID int) (UserModel, error)
	StoreUserModel(ctx context.Context, userID int, modelID int, token string) (int, string, error)
	UpdateUserModel(ctx context.Context, userID int, uuid string, token string) error
	DeleteUserModel(ctx context.Context, userID int, uuid string) error

	ListUserProjektoveOrganizations(ctx context.Context, userID int) ([]UserProjektoveOrganization, error)
	GetUserProjektoveOrganization(ctx context.Context, uuid string) (UserProjektoveOrganization, error)
	GetUserProjektoveOrganizationByID(ctx context.Context, id int) (UserProjektoveOrganization, error)
	UpdateUserOrganizationToken(ctx context.Context, userID int, uuid string, token string) error
	StoreUserProjectoveOrganization(ctx context.Context, userID int, orgID int, token string) (string, error)
	ListOrganizationUsers(ctx context.Context, orgID int) ([]ProjektoveOrganizationUser, error)
	ListProjektoveOrganizations(ctx context.Context) ([]ProjektoveOrganization, error)
	GetProjektoveOrganizationByUUID(ctx context.Context, uuid string) (ProjektoveOrganization, error)
	GetProjektoveOrganizationByName(ctx context.Context, name string) (ProjektoveOrganization, error)
	StoreProjektoveOrganization(ctx context.Context, name, apiURL, browserURL string) (int, string, error)
	UpdateProjektoveOrganization(ctx context.Context, uuid string, apiURL, browserURL string) error
	StoreOrganizationUser(ctx context.Context, orgID int, name string, projektoveID int) error
	DeleteOrganizationUser(ctx context.Context, orgID int, uuid string) error
}

type Tokens struct {
	// oidc
	ID      string
	Refresh string
	// self
	Session string
}

type Auth interface {
	Authenticate(ctx context.Context, token *Tokens) (User, error)
	RegisterRoutes(m *http.ServeMux)
	Middleware(next http.Handler) http.Handler
}
