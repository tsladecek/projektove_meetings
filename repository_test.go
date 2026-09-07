package projektovemeeting

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storeUser(t *testing.T, repo Repository, email string) User {
	t.Helper()
	obj := UserCreate{Email: email, ProjektoveToken: "token", Models: []LLMModel{{Provider: "googleai", Model: "gemini", Token: "g-token"}}}
	id, err := repo.StoreUser(t.Context(), obj)
	require.NoError(t, err)
	return User{ID: id, Email: email}
}

func storeContext(t *testing.T, repo Repository, user User, name string) int {
	t.Helper()
	id, err := repo.StoreContext(t.Context(), user, LLMContextCreate{Name: name, Context: "context"})
	require.NoError(t, err)
	return id
}

func storePrompt(t *testing.T, repo Repository, user User, contextID int) int {
	t.Helper()
	id, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "prompt", Result: "result", ContextID: contextID})
	require.NoError(t, err)
	return id
}

func storePromptAt(t *testing.T, repo Repository, user User, contextID int, provider, model string, status PromptStatus) int {
	t.Helper()
	id, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "prompt", Result: "result", ContextID: contextID, Provider: provider, Model: model, Status: status})
	require.NoError(t, err)
	return id
}

func TestUser(t *testing.T) {
	repo := newRepository(t)
	ctx := t.Context()
	obj := UserCreate{Email: "user@email.com", ProjektoveToken: "token", Models: []LLMModel{{Provider: "googleai", Model: "gemini 3.5", Token: "token"}, {Provider: "openai", Model: "gpt 4.5", Token: "openai token"}}}

	id, err := repo.StoreUser(ctx, obj)
	require.NoError(t, err)
	require.NotZero(t, id)

	user, err := repo.GetUser(ctx, obj.Email)
	require.NoError(t, err)

	assert.Equal(t, id, user.ID)
}

func TestGetUser_NotFound(t *testing.T) {
	repo := newRepository(t)

	_, err := repo.GetUser(t.Context(), "missing@email.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserNotFound))
}

func TestGetUser_WithProviders(t *testing.T) {
	repo := newRepository(t)
	obj := UserCreate{Email: "user@email.com", ProjektoveToken: "token", Models: []LLMModel{
		{Provider: "googleai", Model: "gemini", Token: "g-token"},
		{Provider: "openai", Model: "gpt", Token: "o-token"},
	}}

	id, err := repo.StoreUser(t.Context(), obj)
	require.NoError(t, err)

	user, err := repo.GetUser(t.Context(), obj.Email)
	require.NoError(t, err)
	assert.Equal(t, id, user.ID)
	assert.Equal(t, obj.ProjektoveToken, user.ProjektoveToken)
	require.Len(t, user.LLMModels, 2)
	assert.ElementsMatch(t, stripModelIDs(obj.Models), stripModelIDs(user.LLMModels))
}

// stripModelIDs returns a projection of the models without their DB IDs so
// comparisons don't depend on the concrete values assigned to provider rows.
func stripModelIDs(models []LLMModel) []LLMModel {
	out := make([]LLMModel, 0, len(models))
	for _, m := range models {
		out = append(out, LLMModel{Provider: m.Provider, Model: m.Model, Token: m.Token})
	}
	return out
}

func TestUpdateUser_Token(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	err := repo.UpdateUser(t.Context(), user, UserUpdate{ProjektoveToken: "new-token"})
	require.NoError(t, err)

	updated, err := repo.GetUser(t.Context(), user.Email)
	require.NoError(t, err)
	assert.Equal(t, "new-token", updated.ProjektoveToken)
	assert.Len(t, updated.LLMModels, 1)
}

func TestUpdateUser_Providers(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	replacement := []LLMModel{{Provider: "anthropic", Model: "claude", Token: "a-token"}}
	err := repo.UpdateUser(t.Context(), user, UserUpdate{ProjektoveToken: "new-token", Models: replacement})
	require.NoError(t, err)

	updated, err := repo.GetUser(t.Context(), user.Email)
	require.NoError(t, err)
	assert.Equal(t, "new-token", updated.ProjektoveToken)
	require.Len(t, updated.LLMModels, 1)
	assert.Equal(t, stripModelIDs(replacement), stripModelIDs(updated.LLMModels))
}

func TestUpdateUser_NotFound(t *testing.T) {
	repo := newRepository(t)

	err := repo.UpdateUser(t.Context(), User{ID: 999}, UserUpdate{ProjektoveToken: "token"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserNotFound))
}

func TestStoreContext(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	id, err := repo.StoreContext(t.Context(), user, LLMContextCreate{Name: "c1", Context: "ctx"})
	require.NoError(t, err)
	require.NotZero(t, id)

	got, err := repo.GetContext(t.Context(), user, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "c1", got.Name)
	assert.Equal(t, "ctx", got.Context)
}

func TestGetContext_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	_, err := repo.GetContext(t.Context(), user, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestGetContext_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	id := storeContext(t, repo, owner, "c1")

	_, err := repo.GetContext(t.Context(), other, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestListContexts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	storeContext(t, repo, user, "c1")
	storeContext(t, repo, user, "c2")

	contexts, err := repo.ListContexts(t.Context(), user)
	require.NoError(t, err)
	require.Len(t, contexts, 2)
	assert.ElementsMatch(t, []string{"c1", "c2"}, []string{contexts[0].Name, contexts[1].Name})
}

func TestListContexts_IsolatedByUser(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	storeContext(t, repo, owner, "c1")

	contexts, err := repo.ListContexts(t.Context(), other)
	require.NoError(t, err)
	assert.Empty(t, contexts)
}

func TestDeleteContext(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	id := storeContext(t, repo, user, "c1")

	err := repo.DeleteContext(t.Context(), user, id)
	require.NoError(t, err)

	_, err = repo.GetContext(t.Context(), user, id)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestDeleteContext_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	err := repo.DeleteContext(t.Context(), user, 999)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestDeleteContext_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	id := storeContext(t, repo, owner, "c1")

	err := repo.DeleteContext(t.Context(), other, id)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestDeleteContext_InUse(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	id := storeContext(t, repo, user, "c1")
	storePrompt(t, repo, user, id)

	err := repo.DeleteContext(t.Context(), user, id)
	assert.True(t, errors.Is(err, ErrContextInUse))

	_, err = repo.GetContext(t.Context(), user, id)
	require.NoError(t, err)
}

func TestListPrompts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	contextID := storeContext(t, repo, user, "c1")

	id1, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "p1", Result: "r1", ContextID: contextID})
	require.NoError(t, err)
	require.NotZero(t, id1)
	id2, err := repo.StorePrompt(t.Context(), user, PromptCreate{Prompt: "p2", Result: "r2", ContextID: contextID})
	require.NoError(t, err)
	require.NotZero(t, id2)

	prompts, err := repo.ListPrompts(t.Context(), user)
	require.NoError(t, err)
	require.Len(t, prompts, 2)

	ids := []string{}
	for _, p := range prompts {
		ids = append(ids, p.ID)
	}
	assert.ElementsMatch(t, []string{fmt.Sprint(id1), fmt.Sprint(id2)}, ids)
}

func TestListPrompts_Empty(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	prompts, err := repo.ListPrompts(t.Context(), user)
	require.NoError(t, err)
	assert.Empty(t, prompts)
}

func TestUpdateAndListProjects(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}, {ID: 2, Name: "p2", Description: "d2"}}

	err := repo.UpdateProjectsCache(t.Context(), user, projects)
	require.NoError(t, err)

	entry, err := repo.ListProjects(t.Context(), user)
	require.NoError(t, err)
	assert.ElementsMatch(t, projects, entry.Projects)
	assert.False(t, entry.FetchedAt.IsZero())
}

func TestListProjects_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	_, err := repo.ListProjects(t.Context(), user)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoProjectsFound))
}

func newIssueCreate(parent IssueParent, parentID int) IssueCreate {
	return IssueCreate{
		Parent:       parent,
		ParentID:     parentID,
		Subject:      "subject",
		Description:  "description",
		ProjectID:    1,
		StartDate:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DueDate:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		AssignedToID: 1,
	}
}

func TestStoreIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	require.NotZero(t, issueID)
}

func TestListIssues(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	id1, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	issue2 := newIssueCreate(IssueParentPrompt, promptID)
	issue2.Subject = "subject2"
	id2, err := repo.StoreIssue(t.Context(), user, issue2)
	require.NoError(t, err)

	issues, err := repo.ListIssues(t.Context(), user, IssueParentPrompt, promptID)
	require.NoError(t, err)
	require.Len(t, issues, 2)

	ids := []int{}
	for _, i := range issues {
		ids = append(ids, i.ID)
	}
	assert.ElementsMatch(t, []int{id1, id2}, ids)

	for _, i := range issues {
		assert.Equal(t, IssueParentPrompt, i.Parent)
		assert.Equal(t, promptID, i.ParentID)
		assert.Equal(t, IssueStatusCreated, i.Status)
	}
}

func TestListIssues_ParentNotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	promptID := storePrompt(t, repo, owner, storeContext(t, repo, owner, "c1"))

	_, err := repo.ListIssues(t.Context(), other, IssueParentPrompt, promptID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}

func TestUpdateIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	projektoveID := 42
	obj := IssueUpdate{
		Subject:      "updated",
		Description:  "updated desc",
		ProjectID:    2,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}
	err = repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, issueID, obj)
	require.NoError(t, err)

	issues, err := repo.ListIssues(t.Context(), user, IssueParentPrompt, promptID)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, issueID, issues[0].ID)
	assert.Equal(t, "updated", issues[0].Subject)
	assert.Equal(t, "updated desc", issues[0].Description)
	assert.Equal(t, IssueStatusSubmitted, issues[0].Status)
	require.NotNil(t, issues[0].ProjektoveID)
	assert.Equal(t, projektoveID, *issues[0].ProjektoveID)
}

func TestUpdateIssue_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	err := repo.UpdateIssue(t.Context(), user, IssueParentPrompt, promptID, 999, IssueUpdate{Status: IssueStatusSubmitted})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIssueNotFound))
}

func TestUpdateIssue_ParentNotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	promptID := storePrompt(t, repo, owner, storeContext(t, repo, owner, "c1"))

	err := repo.UpdateIssue(t.Context(), other, IssueParentPrompt, promptID, 1, IssueUpdate{Status: IssueStatusSubmitted})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}

func TestGetPrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(promptID), prompt.ID)
	assert.Equal(t, "prompt", prompt.Prompt)
	assert.Equal(t, "result", prompt.Result)
	assert.Equal(t, "c1", prompt.Context.Name)
}

func TestGetPrompt_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	_, err := repo.GetPrompt(t.Context(), user, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestGetPrompt_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	promptID := storePrompt(t, repo, owner, storeContext(t, repo, owner, "c1"))

	_, err := repo.GetPrompt(t.Context(), other, promptID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestGetUserByID(t *testing.T) {
	repo := newRepository(t)
	obj := UserCreate{Email: "user@email.com", ProjektoveToken: "token", Models: []LLMModel{{Provider: "googleai", Model: "gemini", Token: "g-token"}}}

	id, err := repo.StoreUser(t.Context(), obj)
	require.NoError(t, err)

	user, err := repo.GetUserByID(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, id, user.ID)
	assert.Equal(t, obj.Email, user.Email)
	assert.Equal(t, obj.ProjektoveToken, user.ProjektoveToken)
	require.Len(t, user.LLMModels, 1)
	assert.Equal(t, "gemini", user.LLMModels[0].Model)
}

func TestGetUserByID_NotFound(t *testing.T) {
	repo := newRepository(t)

	_, err := repo.GetUserByID(t.Context(), 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserNotFound))
}

func TestSetPromptProcessing(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	err := repo.SetPromptProcessing(t.Context(), user, promptID, "full prompt")
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusProcessing, prompt.Status)
	assert.Equal(t, "full prompt", prompt.Prompt)
}

func TestSetPromptProcessing_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	err := repo.SetPromptProcessing(t.Context(), user, 999, "prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestCompletePrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	err := repo.CompletePrompt(t.Context(), user, promptID, PromptComplete{
		Status: PromptStatusDone,
		Prompt: "p",
		Result: "r",
	})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusDone, prompt.Status)
	assert.Equal(t, "p", prompt.Prompt)
	assert.Equal(t, "r", prompt.Result)
	assert.Empty(t, prompt.Error)
}

func TestCompletePrompt_Error(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	err := repo.CompletePrompt(t.Context(), user, promptID, PromptComplete{
		Status: PromptStatusError,
		Error:  "boom",
	})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), user, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusError, prompt.Status)
	assert.Equal(t, "boom", prompt.Error)
}

func TestCompletePrompt_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	err := repo.CompletePrompt(t.Context(), user, 999, PromptComplete{Status: PromptStatusDone})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestEnqueueTask(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 1, PromptID: 2}})
	require.NoError(t, err)
	require.NotZero(t, id)

	task, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, id, task.ID)
	assert.Equal(t, TaskTypeInference, task.Type)
	assert.Equal(t, TaskStatusProcessing, task.Status)
	assert.Equal(t, InferenceJob{UserID: 1, PromptID: 2}, task.Payload)
}

func TestClaimTask_Empty(t *testing.T) {
	repo := newRepository(t)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestClaimTask_Exclusive(t *testing.T) {
	repo := newRepository(t)

	id1, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 1, PromptID: 1}})
	require.NoError(t, err)
	id2, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 2, PromptID: 2}})
	require.NoError(t, err)

	task1, ok1, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok1)
	task2, ok2, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok2)
	assert.NotEqual(t, task1.ID, task2.ID)

	// a claimed task must not be claimable again
	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)

	ids := []int{task1.ID, task2.ID}
	assert.ElementsMatch(t, []int{id1, id2}, ids)
}

func TestCompleteTask(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 1, PromptID: 1}})
	require.NoError(t, err)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)

	err = repo.CompleteTask(t.Context(), id, TaskStatusDone, "")
	require.NoError(t, err)

	// done tasks are not claimable
	_, ok, err = repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCompleteTask_Failure(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 1, PromptID: 1}})
	require.NoError(t, err)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)

	err = repo.CompleteTask(t.Context(), id, TaskStatusFailed, "boom")
	require.NoError(t, err)

	// failed tasks are not claimable either
	_, ok, err = repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestResetOrphanedTasks(t *testing.T) {
	repo := newRepository(t)

	doneID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 1, PromptID: 1}})
	require.NoError(t, err)
	orphanID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 2, PromptID: 2}})
	require.NoError(t, err)
	failedID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: 3, PromptID: 3}})
	require.NoError(t, err)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, repo.CompleteTask(t.Context(), doneID, TaskStatusDone, ""))
	require.NoError(t, repo.CompleteTask(t.Context(), failedID, TaskStatusFailed, "boom"))

	require.NoError(t, repo.ResetOrphanedTasks(t.Context()))

	// the reset task becomes pending and claimable again
	task, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, orphanID, task.ID)
}

func TestGetIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))
	issueID, err := repo.StoreIssue(t.Context(), user, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	issue, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, issueID, issue.ID)
	assert.Equal(t, "subject", issue.Subject)
	assert.Equal(t, IssueStatusCreated, issue.Status)
}

func TestGetIssue_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	promptID := storePrompt(t, repo, user, storeContext(t, repo, user, "c1"))

	_, err := repo.GetIssue(t.Context(), user, IssueParentPrompt, promptID, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIssueNotFound))
}

func TestGetIssue_ParentNotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	promptID := storePrompt(t, repo, owner, storeContext(t, repo, owner, "c1"))

	_, err := repo.GetIssue(t.Context(), other, IssueParentPrompt, promptID, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}
