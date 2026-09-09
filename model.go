package projektovemeeting

import (
	"time"
)

type ProjektoveStatus int

const (
	ProjektoveStatusNew ProjektoveStatus = iota + 1
	ProjektoveStatusInProgress
	ProjektoveStatusWaitingOurSide
	ProjektoveStatusWaitingClientSide
	ProjektoveStatusPlanned
	ProjektoveStatusSolved
	ProjektoveStatusFuture
	ProjektoveStatusClosed
	ProjektoveStatusRejected
	ProjektoveStatusWaitingDistributorSide
)

type IssueParent string

const (
	IssueParentPrompt IssueParent = "prompt"
	IssueParentBatch  IssueParent = "batch"
)

type IssueStatus string

const (
	IssueStatusCreated      IssueStatus = "created"
	IssueStatusSubmitted    IssueStatus = "submitted"
	IssueStatusSubmitFailed IssueStatus = "submit_failed"
	IssueStatusIgnored      IssueStatus = "ignored"
)

type PromptStatus string

const (
	PromptStatusCreated    PromptStatus = "created"
	PromptStatusProcessing PromptStatus = "processing"
	PromptStatusDone       PromptStatus = "done"
	PromptStatusError      PromptStatus = "error"
)

func (s PromptStatus) IsPending() bool {
	return s == PromptStatusCreated || s == PromptStatusProcessing
}

type TaskType string

const (
	TaskTypeInference TaskType = "inference"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
)

type InferenceJob struct {
	UserProjektoveOrganizationID int `json:"user_projektove_organization_id"`
	PromptID                     int `json:"prompt_id"`
}

type TaskCreate struct {
	Type    TaskType
	Payload InferenceJob
}

type Task struct {
	ID      int
	Type    TaskType
	Payload InferenceJob
	Status  TaskStatus
	Error   string
}

type ProjektoveProject struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProjektoveIssueStatus struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsClosed bool   `json:"isClosed"`
	Role     string `json:"role"`
}

type ProjektoveUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ProjektoveIssueTracker struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ProjektoveIssue struct {
	ID          int                    `json:"id"`
	Project     ProjektoveProject      `json:"project"`
	Status      ProjektoveIssueStatus  `json:"status"`
	Author      ProjektoveUser         `json:"author"`
	AssignedTo  *ProjektoveUser        `json:"assignedTo"`
	Subject     string                 `json:"subject"`
	Description string                 `json:"description"`
	DueDate     time.Time              `json:"dueDate"`
	CreatedOn   time.Time              `json:"createdOn"`
	Tracker     ProjektoveIssueTracker `json:"tracker"`
}

type ProjektoveIssueCreate struct {
	Subject      string    `json:"subject"`
	Description  string    `json:"description,omitempty"`
	ProjectID    int       `json:"project_id"`
	StartDate    time.Time `json:"start_date,omitzero"`
	DueDate      time.Time `json:"due_date,omitzero"`
	AssignedToID int       `json:"assigned_to_id"`
}

type Provider struct {
	ID       int
	Provider string
}

type Model struct {
	ID         int
	UUID       string
	ProviderID int
	Provider   string
	Model      string
}

type UserModel struct {
	ID       int
	UUID     string
	UserID   int
	ModelID  int
	Provider string
	Model    string
	Token    string
}

type ProjektoveOrganization struct {
	ID         int
	Name       string
	APIURL     string
	BrowserURL string
}

type ProjektoveOrganizationUser struct {
	ID             int
	OrganizationID int
	Name           string
	ProjektoveID   int
}

type UserProjektoveOrganization struct {
	ID             int
	UUID           string
	UserID         int
	OrganizationID int
	Token          string
	OrgName        string
	APIURL         string
	BrowserURL     string
}

type User struct {
	ID           int
	IsAdmin      bool
	Email        string
	PasswordHash *string
}

type Issue struct {
	ID           int
	UUID         string
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

type Prompt struct {
	ID                            int
	UUID                          string
	Prompt                        string
	Result                        string
	Error                         string
	Context                       LLMContext
	Status                        PromptStatus
	ModelID                       int
	Provider                      string
	Model                         string
	FileContent                   string
	CreatedAt                     time.Time
	UserProjektoveOrganizationID  int
	TotalIssues                   int
	SubmittedIssues               int
}

type LLMContext struct {
	ID      int
	UUID    string
	Context string
	Name    string
}

type Batch struct {
	ID                           int
	UUID                         string
	FileContent                  string
	CreatedAt                    time.Time
	UserProjektoveOrganizationID int
	TotalIssues                  int
	SubmittedIssues              int
}
