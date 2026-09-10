package projektovemeeting

import "time"

type IssueCreate struct {
	Parent       IssueParent `json:"parent"`
	ParentID     int         `json:"parent_id"`
	Subject      string      `json:"subject"`
	Description  string      `json:"description"`
	ProjectID    int         `json:"project_id"`
	StartDate    time.Time   `json:"start_date"`
	DueDate      time.Time   `json:"due_date"`
	AssignedToID int         `json:"assigned_to_id"`
}

func (i IssueCreate) ToDomain(id int) Issue {
	return Issue{
		ID:           id,
		Parent:       i.Parent,
		ParentID:     i.ParentID,
		Subject:      i.Subject,
		Description:  i.Description,
		ProjectID:    i.ProjectID,
		StartDate:    i.StartDate,
		DueDate:      i.DueDate,
		AssignedToID: i.AssignedToID,
		Status:       IssueStatusCreated,
	}

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

type UserCreate struct {
	Email        string
	PasswordHash *string
	IsAdmin      bool
}

type UserUpdate struct {
	IsAdmin *bool
}

type ProjectsCacheEntry struct {
	Projects            []ProjektoveProject
	FetchedAt           time.Time
	UserProjektoveOrgID int
}

type LLMContextCreate struct {
	Context string
	Name    string
}

type PromptCreate struct {
	Prompt                       string
	Result                       string
	Error                        error
	PromptContext                string
	ContextID                    int
	Status                       PromptStatus
	ModelID                      int
	FileContent                  string
	CreatedAt                    time.Time
	UserProjektoveOrganizationID int
}

type PromptComplete struct {
	Prompt string
	Result string
	Error  string
	Status PromptStatus
}

type UserModelView struct {
	ID       string
	Provider string
	Model    string
	Token    string
}

type UserOrgView struct {
	ID    string
	Name  string
	Token string
}

type ContextView struct {
	ID      string
	Name    string
	Context string
}

type UserProfileView struct {
	Email                  string
	Models                 []UserModelView
	AvailableModels        []Model
	Organizations          []UserOrgView
	AvailableOrganizations []ProjektoveOrganization
	Contexts               []ContextView
}

type UserUpdateView struct {
	Models        []UserModelView
	Organizations []UserOrgTokenView
}

type UserOrgTokenView struct {
	ID               string
	OrganizationName string
	Token            string
}

type IssueView struct {
	ID           string
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AssignedToID int
	ProjektoveID *int
	Status       IssueStatus
	Editable     bool
	Error        string
}

type PromptView struct {
	ID            string
	Prompt        string
	Result        string
	Error         string
	PromptContext string
	ContextName   string
	Status        PromptStatus
	Model         string
	OrgID         string
	Issues        []IssueView
}

type IssueUpdateView struct {
	Subject      string
	Description  string
	ProjectID    int
	StartDate    time.Time
	DueDate      time.Time
	AssignedToID int
}

type ProjectOptionView struct {
	ID   int
	Name string
}

type PromptListItem struct {
	ID              string
	PromptContext   string
	ContextName     string
	Status          PromptStatus
	CreatedAt       time.Time
	TotalIssues     int
	SubmittedIssues int
}

type PromptListView struct {
	Items      []PromptListItem
	HasMore    bool
	NextOffset int
}

type BatchListItem struct {
	ID              string
	CreatedAt       time.Time
	TotalIssues     int
	SubmittedIssues int
}

type BatchListView struct {
	Items      []BatchListItem
	HasMore    bool
	NextOffset int
}

type BatchView struct {
	ID          string
	FileContent string
	CreatedAt   time.Time
	OrgID       string
	Issues      []IssueView
}

type AdminOrganizationView struct {
	ID         string
	Name       string
	APIURL     string
	BrowserURL string
	Users      []ProjektoveOrganizationUser
}
