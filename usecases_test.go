package projektovemeeting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProjektove struct {
	createResult   ProjektoveIssue
	createErr      error
	created        []ProjektoveIssueCreate
	getProjectsRes []ProjektoveProject
	getProjectsErr error
}

func (f *fakeProjektove) CreateIssue(ctx context.Context, user User, obj ProjektoveIssueCreate) (ProjektoveIssue, error) {
	f.created = append(f.created, obj)
	if f.createErr != nil {
		return ProjektoveIssue{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjektove) GetProjects(ctx context.Context, user User) ([]ProjektoveProject, error) {
	return f.getProjectsRes, f.getProjectsErr
}

type fakeLLM struct {
	result string
	err    error
}

func (f *fakeLLM) Infer(ctx context.Context, prompt string) (string, error) {
	return f.result, f.err
}

func TestControllerGetPrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, promptUUID := storePrompt(t, repo, user, contextID)

	createdID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	created, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, createdID)
	require.NoError(t, err)

	submitted := newIssueCreate(IssueParentPrompt, promptID)
	submitted.Subject = "submitted subject"
	submittedID, err := repo.StoreIssue(t.Context(), user, submitted)
	require.NoError(t, err)
	submittedFromDB, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, submittedID)
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
	view, err := c.GetPrompt(t.Context(), user, promptUUID)
	require.NoError(t, err)
	assert.Equal(t, promptUUID, view.ID)
	assert.Equal(t, "c1", view.ContextName)
	require.Len(t, view.Issues, 2)

	byID := map[string]IssueView{}
	for _, iss := range view.Issues {
		byID[iss.ID] = iss
	}

	assert.Equal(t, IssueStatusCreated, byID[created.UUID].Status)
	assert.True(t, byID[created.UUID].Editable)

	assert.Equal(t, IssueStatusSubmitted, byID[submittedFromDB.UUID].Status)
	assert.False(t, byID[submittedFromDB.UUID].Editable)
	require.NotNil(t, byID[submittedFromDB.UUID].ProjektoveID)
	assert.Equal(t, projektoveID, *byID[submittedFromDB.UUID].ProjektoveID)
}

func TestControllerUpdateIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	c := Controller{Repository: repo}
	err = c.UpdateIssue(t.Context(), user, iss.UUID, IssueUpdateView{
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
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
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
	err = c.UpdateIssue(t.Context(), user, iss.UUID, IssueUpdateView{Subject: "nope"})
	assert.True(t, errors.Is(err, ErrIssueSubmitted))
}

func TestSubmitIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)

	p := &fakeProjektove{createResult: ProjektoveIssue{ID: 7}}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, iss.UUID)
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
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)

	p := &fakeProjektove{createErr: errors.New("boom")}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, iss.UUID)
	require.Error(t, err)

	got, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, IssueStatusSubmitFailed, got.Status)
	assert.Nil(t, got.ProjektoveID)
}

func TestSubmitIssue_Submitted(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
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

	err = c.SubmitIssue(t.Context(), user, iss.UUID)
	assert.True(t, errors.Is(err, ErrIssueSubmitted))
	assert.Empty(t, p.created)
}

func TestSubmitIssue_Incomplete(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, issueID, IssueUpdate{
		Subject:      "subject",
		Description:  "description",
		ProjectID:    1,
		StartDate:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DueDate:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		AssignedToID: 0,
		Status:       IssueStatusCreated,
	}))

	p := &fakeProjektove{}
	c := Controller{Repository: repo, Projektove: p}

	err = c.SubmitIssue(t.Context(), user, iss.UUID)
	assert.True(t, errors.Is(err, ErrIssueIncomplete))
	assert.Empty(t, p.created)

	got, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, IssueStatusCreated, got.Status)
}

func TestControllerListPrompts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")

	olderID, olderUUID, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "older", Result: "r", ContextID: contextID, Status: PromptStatusError})
	require.NoError(t, err)
	_, newerUUID, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "newer", Result: "r", ContextID: contextID, Status: PromptStatusDone})
	require.NoError(t, err)

	submitted := newIssueCreate(IssueParentPrompt, olderID)
	submittedID, err := repo.StoreIssue(t.Context(), user, submitted)
	require.NoError(t, err)
	require.NotZero(t, submittedID)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentPrompt, olderID, submittedID, IssueUpdate{
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

	// first page with everything
	view, err := c.ListPrompts(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, view.Items, 2)
	assert.False(t, view.HasMore)

	// newest first
	assert.Equal(t, newerUUID, view.Items[0].ID)
	assert.True(t, view.Items[0].CreatedAt.After(view.Items[1].CreatedAt))
	assert.Equal(t, PromptStatusDone, view.Items[0].Status)

	assert.Equal(t, olderUUID, view.Items[1].ID)
	assert.Equal(t, "c1", view.Items[1].ContextName)
	assert.Equal(t, PromptStatusError, view.Items[1].Status)
	assert.Equal(t, 1, view.Items[1].TotalIssues)
	assert.Equal(t, 1, view.Items[1].SubmittedIssues)

	// pagination: limit 1 -> newest only + HasMore, next offset points at the rest
	page, err := c.ListPrompts(t.Context(), user, 1, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.True(t, page.HasMore)
	assert.Equal(t, 1, page.NextOffset)
	assert.Equal(t, newerUUID, page.Items[0].ID)

	rest, err := c.ListPrompts(t.Context(), user, 1, page.NextOffset)
	require.NoError(t, err)
	require.Len(t, rest.Items, 1)
	assert.False(t, rest.HasMore)
	assert.Equal(t, olderUUID, rest.Items[0].ID)
}

func TestControllerCreatePrompt_EnqueuesTask(t *testing.T) {
	repo, txp := newAppRepos(t)
	storeUser(t, repo, "user@email.com")
	user, err := repo.GetUser(t.Context(), "user@email.com")
	require.NoError(t, err)
	contextID, _ := storeContext(t, repo, user, "c1")

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsRes: []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}},
		Users:      ProjektoveUsers{{ID: 1, Name: "u1"}},
	}

	promptUUID, err := c.CreatePrompt(t.Context(), user, "googleai", "gemini", contextID, "meeting notes")
	require.NoError(t, err)
	require.NotEmpty(t, promptUUID)

	prompt, err := repo.GetPromptByUUID(t.Context(), user, promptUUID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusCreated, prompt.Status)
	assert.Equal(t, "googleai", prompt.Provider)
	assert.Equal(t, "gemini", prompt.Model)
	assert.Equal(t, "meeting notes", prompt.FileContent)
	assert.Empty(t, prompt.Prompt)
	assert.Empty(t, prompt.Result)

	task, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, TaskTypeInference, task.Type)
	assert.Equal(t, InferenceJob{UserID: user.ID, PromptID: prompt.ID}, task.Payload)
}

func TestControllerCreatePrompt_ModelNotFound(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsRes: []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}},
	}

	_, err := c.CreatePrompt(t.Context(), user, "nope", "model", contextID, "meeting notes")
	assert.True(t, errors.Is(err, ErrModelNotFound))

	// nothing was stored since the transaction was aborted before the task
	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestControllerCreatePrompt_MissingToken(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")

	c := Controller{Repository: repo, TxProvider: txp, Projektove: &fakeProjektove{getProjectsErr: ErrProjektoveTokenNotConfigured}}

	_, err := c.CreatePrompt(t.Context(), user, "googleai", "gemini", contextID, "meeting notes")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))

	// no prompt and no task were stored
	prompts, _, err := repo.ListPrompts(t.Context(), user, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, prompts)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRunInference(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePromptAt(t, repo, user, contextID, "googleai", "gemini", PromptStatusCreated)

	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}
	p := &fakeProjektove{getProjectsRes: projects}
	llm := &fakeLLM{result: `[{"subject": "t1", "description": "d1", "project_id": 1, "assigned_to_id": 1}]`}

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: p,
		Users:      ProjektoveUsers{{ID: 1, Name: "u1"}},
		NewLLMProvider: func(provider LLMProvider, model string, token string) (LLM, error) {
			return llm, nil
		},
	}

	err := c.RunInference(t.Context(), InferenceJob{UserID: user.ID, PromptID: promptID})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusDone, prompt.Status)
	assert.NotEmpty(t, prompt.Prompt)
	assert.NotEmpty(t, prompt.Result)
	assert.Empty(t, prompt.Error)
	assert.Contains(t, prompt.Prompt, "p1")
	assert.Contains(t, prompt.Prompt, "u1")

	issues, err := repo.ListIssues(t.Context(), user, IssueParentPrompt, promptID)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "t1", issues[0].Subject)
	assert.Equal(t, 1, issues[0].ProjectID)
}

func TestRunInference_SkipsNonPending(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, user, contextID)

	c := Controller{Repository: repo, TxProvider: txp, Projektove: &fakeProjektove{}, NewLLMProvider: func(provider LLMProvider, model string, token string) (LLM, error) {
		return &fakeLLM{}, nil
	}}

	err := c.RunInference(t.Context(), InferenceJob{UserID: user.ID, PromptID: promptID})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusDone, prompt.Status)
	assert.Equal(t, "prompt", prompt.Prompt)
}

func TestRunInference_LLMError(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePromptAt(t, repo, user, contextID, "googleai", "gemini", PromptStatusCreated)

	p := &fakeProjektove{getProjectsRes: []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}}
	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: p,
		NewLLMProvider: func(provider LLMProvider, model string, token string) (LLM, error) {
			return &fakeLLM{err: errors.New("boom")}, nil
		},
	}

	err := c.RunInference(t.Context(), InferenceJob{UserID: user.ID, PromptID: promptID})
	require.Error(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusError, prompt.Status)
	assert.Equal(t, "boom", prompt.Error)
	assert.Empty(t, prompt.Result)
}

func TestRunInference_UnmarshalError(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePromptAt(t, repo, user, contextID, "googleai", "gemini", PromptStatusCreated)

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsRes: []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}},
		NewLLMProvider: func(provider LLMProvider, model string, token string) (LLM, error) {
			return &fakeLLM{result: "not json"}, nil
		},
	}

	err := c.RunInference(t.Context(), InferenceJob{UserID: user.ID, PromptID: promptID})
	require.Error(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusError, prompt.Status)
	assert.Contains(t, prompt.Error, "when unmarshalling response")
}

func TestRunInference_ProjectsError(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePromptAt(t, repo, user, contextID, "googleai", "gemini", PromptStatusCreated)

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsErr: errors.New("net down")},
		NewLLMProvider: func(provider LLMProvider, model string, token string) (LLM, error) {
			return &fakeLLM{}, nil
		},
	}

	err := c.RunInference(t.Context(), InferenceJob{UserID: user.ID, PromptID: promptID})
	require.Error(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusError, prompt.Status)
	assert.Contains(t, prompt.Error, "when listing projects")
}

func TestControllerCreateBatch(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsRes: []ProjektoveProject{
			{ID: 1, Name: "p1", Description: "d1"},
			{ID: 2, Name: "p2", Description: "d2"},
		}},
		Users: ProjektoveUsers{{ID: 1, Name: "u1"}},
	}

	batchUUID, err := c.CreateBatch(t.Context(), user, validCSV())
	require.NoError(t, err)
	require.NotEmpty(t, batchUUID)

	batch, err := repo.GetBatchByUUID(t.Context(), user, batchUUID)
	require.NoError(t, err)
	assert.Equal(t, validCSV(), batch.FileContent)

	issues, err := repo.ListIssues(t.Context(), user, IssueParentBatch, batch.ID)
	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, "task one", issues[0].Subject)
	assert.Equal(t, IssueParentBatch, issues[0].Parent)
	assert.Equal(t, batch.ID, issues[0].ParentID)
	assert.Equal(t, 1, issues[0].ProjectID)
	assert.Equal(t, 1, issues[0].AssignedToID)
	assert.Zero(t, issues[1].AssignedToID)
}

func TestControllerCreateBatch_InvalidCSV(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")

	c := Controller{
		Repository: repo,
		TxProvider: txp,
		Projektove: &fakeProjektove{getProjectsRes: []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}},
		Users:      ProjektoveUsers{{ID: 1, Name: "u1"}},
	}

	raw := "Subject, Project, Start date, Due date\n" +
		"x, nope, 2026-01-01, 2026-02-01\n"

	_, err := c.CreateBatch(t.Context(), user, raw)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchCSVInvalid))

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, batches)
	assert.False(t, hasMore)
}

func TestControllerCreateBatch_ProjectsError(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")

	c := Controller{Repository: repo, TxProvider: txp, Projektove: &fakeProjektove{getProjectsErr: errors.New("net down")}}

	_, err := c.CreateBatch(t.Context(), user, validCSV())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "when listing projects")
}

func TestControllerCreateBatch_MissingToken(t *testing.T) {
	repo, txp := newAppRepos(t)
	user := storeUser(t, repo, "user@email.com")

	c := Controller{Repository: repo, TxProvider: txp, Projektove: &fakeProjektove{getProjectsErr: ErrProjektoveTokenNotConfigured}}

	_, err := c.CreateBatch(t.Context(), user, validCSV())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, batches)
	assert.False(t, hasMore)
}

func TestControllerListBatches(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	batchID, batchUUID := storeBatch(t, repo, user, "raw")

	submitted := newIssueCreate(IssueParentBatch, batchID)
	submittedID, err := repo.StoreIssue(t.Context(), user, submitted)
	require.NoError(t, err)
	require.NotZero(t, submittedID)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), user, IssueParentBatch, batchID, submittedID, IssueUpdate{
		Subject:      submitted.Subject,
		Description:  submitted.Description,
		ProjectID:    submitted.ProjectID,
		StartDate:    submitted.StartDate,
		DueDate:      submitted.DueDate,
		AssignedToID: submitted.AssignedToID,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))
	_, err = repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentBatch, batchID))
	require.NoError(t, err)

	c := Controller{Repository: repo}

	view, err := c.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, view.Items, 1)
	assert.False(t, view.HasMore)
	assert.Equal(t, batchUUID, view.Items[0].ID)
	assert.Equal(t, 2, view.Items[0].TotalIssues)
	assert.Equal(t, 1, view.Items[0].SubmittedIssues)

	page, err := c.ListBatches(t.Context(), user, 1, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, 1, page.NextOffset)
	assert.False(t, page.HasMore)
}

func TestControllerGetBatch(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	batchID, batchUUID := storeBatch(t, repo, user, validCSV())

	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentBatch, batchID))
	require.NoError(t, err)
	require.NotZero(t, issueID)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentBatch, batchID, issueID)
	require.NoError(t, err)

	c := Controller{Repository: repo}

	view, err := c.GetBatch(t.Context(), user, batchUUID)
	require.NoError(t, err)
	assert.Equal(t, batchUUID, view.ID)
	assert.Equal(t, validCSV(), view.FileContent)
	require.Len(t, view.Issues, 1)
	assert.Equal(t, iss.UUID, view.Issues[0].ID)
	assert.True(t, view.Issues[0].Editable)
}

func TestControllerGetBatch_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	c := Controller{Repository: repo}
	_, err := c.GetBatch(t.Context(), user, "missing")
	assert.True(t, errors.Is(err, ErrBatchNotFound))
}

func TestControllerGetIssueViewByUUID(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	batchID, _ := storeBatch(t, repo, user, "raw")

	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentBatch, batchID))
	require.NoError(t, err)
	iss, err := repo.GetIssue(t.Context(), user, IssueParentBatch, batchID, issueID)
	require.NoError(t, err)

	c := Controller{Repository: repo}
	view, err := c.GetIssueViewByUUID(t.Context(), user, iss.UUID)
	require.NoError(t, err)
	assert.Equal(t, iss.UUID, view.ID)
	assert.Equal(t, "subject", view.Subject)
	assert.Equal(t, IssueStatusCreated, view.Status)
}
