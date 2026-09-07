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
	Email           string
	ProjektoveToken string
	Models          []LLMModel
}

type UserUpdate struct {
	ProjektoveToken string
	UpdateModels    bool
	Models          []LLMModel
}

type ProjectsCacheEntry struct {
	Projects  []ProjektoveProject
	FetchedAt time.Time
}

type LLMContextCreate struct {
	Context string
	Name    string
}

type PromptCreate struct {
	Prompt      string
	Result      string
	Error       error
	ContextID   int
	Status      PromptStatus
	Provider    string
	Model       string
	FileContent string
	CreatedAt   time.Time
}

type PromptComplete struct {
	Prompt string
	Result string
	Error  string
	Status PromptStatus
}

type LLMModelView struct {
	Provider LLMProvider
	Model    string
	Token    string
}

type ContextView struct {
	ID      int
	Name    string
	Context string
}

type UserProfileView struct {
	Email           string
	ProjektoveToken string
	Models          []LLMModelView
	Contexts        []ContextView
}

type UserUpdateView struct {
	ProjektoveToken string
	Models          []LLMModelView
}

type IssueView struct {
	ID           int
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
	ID          string
	Prompt      string
	Result      string
	Error       string
	ContextName string
	Status      PromptStatus
	Issues      []IssueView
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
