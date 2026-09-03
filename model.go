package projektovemeeting

import (
	"fmt"
	"strings"
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

func (pi ProjektoveIssue) Link() string {
	return fmt.Sprintf("https://app.projektove.cz/%s/tasks/%d", strings.ToLower(pi.Tracker.Name), pi.ID)
}

type ProjektoveIssueCreate struct {
	Subject      string    `json:"subject"`
	Description  string    `json:"description,omitempty"`
	ProjectID    int       `json:"project_id"`
	StartDate    time.Time `json:"start_date,omitzero"`
	DueDate      time.Time `json:"due_date,omitzero"`
	AuthorID     int       `json:"author_id"`
	AssignedToID int       `json:"assigned_to_id"`
}

type LLMModel struct {
	Provider LLMProvider
	Model    string
	Token    string
}

type User struct {
	ID              int
	IsAdmin         bool
	Email           string
	ProjektoveToken string
	LLMModels       []LLMModel
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
