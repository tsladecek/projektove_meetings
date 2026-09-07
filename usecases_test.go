package projektovemeeting

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProjektove struct {
	createResult ProjektoveIssue
	createErr    error
	created      []ProjektoveIssueCreate
}

func (f *fakeProjektove) CreateIssue(ctx context.Context, user User, obj ProjektoveIssueCreate) (ProjektoveIssue, error) {
	f.created = append(f.created, obj)
	if f.createErr != nil {
		return ProjektoveIssue{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjektove) GetProjects(ctx context.Context, user User) ([]ProjektoveProject, error) {
	return nil, nil
}

func TestControllerGetPrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	createdID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	submitted := newIssueCreate(IssueParentPrompt, promptID)
	submitted.Subject = "submitted subject"
	submittedID, err := repo.StoreIssue(t.Context(), user, submitted)
	require.NoError(t, err)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, submittedID, IssueUpdate{
		Subject:      submitted.Subject,
		Description:  submitted.Description,
		ProjectID:    submitted.ProjectID,
		StartDate:    submitted.StartDate,
		DueDate:      submitted.DueDate,
		AssignedToID: submitted.AssignedToID,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))

	c := Controller{Repository: repo}
	view, err := c.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(promptID), view.ID)
	assert.Equal(t, "c1", view.ContextName)
	require.Len(t, view.Issues, 2)

	byID := map[int]IssueView{}
	for _, iss := range view.Issues {
		byID[iss.ID] = iss
	}

	assert.Equal(t, IssueStatusCreated, byID[createdID].Status)
	assert.True(t, byID[createdID].Editable)

	assert.Equal(t, IssueStatusSubmitted, byID[submittedID].Status)
	assert.False(t, byID[submittedID].Editable)
	require.NotNil(t, byID[submittedID].ProjektoveID)
	assert.Equal(t, projektoveID, *byID[submittedID].ProjektoveID)
}

func TestControllerUpdateIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	c := Controller{Repository: repo}
	err = c.UpdateIssue(t.Context(), user, promptID, issueID, IssueUpdateView{
		Subject:      "new subject",
		Description:  "new description",
		ProjectID:    2,
		AssignedToID: 3,
		StartDate:    start,
		DueDate:      due,
	})
	require.NoError(t, err)

	got, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, "new subject", got.Subject)
	assert.Equal(t, "new description", got.Description)
	assert.Equal(t, 2, got.ProjectID)
	assert.Equal(t, 3, got.AssignedToID)
	assert.Equal(t, start, got.StartDate)
	assert.Equal(t, due, got.DueDate)
	assert.Equal(t, IssueStatusCreated, got.Status)
}

func TestUpdateIssue_Submitted(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, issueID, IssueUpdate{
		Subject:      "subject",
		Description:  "description",
		ProjectID:    1,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))

	c := Controller{Repository: repo}
	err = c.UpdateIssue(t.Context(), user, promptID, issueID, IssueUpdateView{Subject: "nope"})
	assert.True(t, errors.Is(err, ErrIssueSubmitted))
}

func TestSubmitIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	p := &fakeProjektove{createResult: ProjektoveIssue{ID: 7}}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, promptID, issueID)
	require.NoError(t, err)

	require.Len(t, p.created, 1)
	assert.Equal(t, "subject", p.created[0].Subject)
	assert.Equal(t, 1, p.created[0].ProjectID)

	got, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, IssueStatusSubmitted, got.Status)
	require.NotNil(t, got.ProjektoveID)
	assert.Equal(t, 7, *got.ProjektoveID)
}

func TestSubmitIssue_Failure(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	p := &fakeProjektove{createErr: errors.New("boom")}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, promptID, issueID)
	require.Error(t, err)

	got, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, IssueStatusSubmitFailed, got.Status)
	assert.Nil(t, got.ProjektoveID)
}

func TestSubmitIssue_Submitted(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, issueID, IssueUpdate{
		Subject:      "subject",
		Description:  "description",
		ProjectID:    1,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))

	p := &fakeProjektove{}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, promptID, issueID)
	assert.True(t, errors.Is(err, ErrIssueSubmitted))
	assert.Empty(t, p.created)
}
