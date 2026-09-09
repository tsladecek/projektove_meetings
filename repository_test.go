package projektovemeeting

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storeUser(t *testing.T, repo Repository, email string) User {
	t.Helper()
	obj := UserCreate{Email: email}
	id, err := repo.StoreUser(t.Context(), obj)
	require.NoError(t, err)
	return User{ID: id, Email: email}
}

func storeOrg(t *testing.T, repo Repository, user User) UserProjektoveOrganization {
	t.Helper()
	r, ok := repo.(*RepositorySqlite)
	require.True(t, ok, "expected *RepositorySqlite")

	name := "org-" + user.Email + "-" + newUUID()
	result, err := r.DB.ExecContext(t.Context(), "INSERT INTO projektove_organizations (name, api_url, browser_url) VALUES (?, ?, ?)", name, "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	orgID, err := result.LastInsertId()
	require.NoError(t, err)

	if _, err := r.DB.ExecContext(t.Context(), "INSERT INTO users_projektove_organizations (uuid, user_id, organization_id, token) VALUES (?, ?, ?, ?)", newUUID(), user.ID, orgID, "token"); err != nil {
		t.Fatalf("failed to store user organization: %v", err)
	}

	orgs, err := r.ListUserProjektoveOrganizations(t.Context(), user.ID)
	require.NoError(t, err)
	for _, o := range orgs {
		if o.OrganizationID == int(orgID) {
			return o
		}
	}
	t.Fatalf("failed to load stored organization")
	return UserProjektoveOrganization{}
}

func storeOrgUser(t *testing.T, repo Repository, org UserProjektoveOrganization, name string, projektoveID int) {
	t.Helper()
	r, ok := repo.(*RepositorySqlite)
	require.True(t, ok, "expected *RepositorySqlite")
	if _, err := r.DB.ExecContext(t.Context(), "INSERT INTO projektove_organizations_users (organization_id, name, projektove_id) VALUES (?, ?, ?)", org.OrganizationID, name, projektoveID); err != nil {
		t.Fatalf("failed to store organization user: %v", err)
	}
}

func storeUserModel(t *testing.T, repo Repository, user User, provider, model, token string) UserModel {
	t.Helper()
	providerID, err := repo.GetOrCreateProvider(t.Context(), provider)
	require.NoError(t, err)
	modelID, _, err := repo.GetOrCreateModel(t.Context(), providerID, model)
	require.NoError(t, err)
	_, uuid, err := repo.StoreUserModel(t.Context(), user.ID, modelID, token)
	require.NoError(t, err)
	um, err := repo.GetUserModelByUUID(t.Context(), user.ID, uuid)
	require.NoError(t, err)
	return um
}

func storeContext(t *testing.T, repo Repository, user User, name string) (int, string) {
	t.Helper()
	id, uuid, err := repo.StoreContext(t.Context(), user, LLMContextCreate{Name: name, Context: "context"})
	require.NoError(t, err)
	return id, uuid
}

func storePrompt(t *testing.T, repo Repository, org UserProjektoveOrganization, contextID int, modelID int) (int, string) {
	t.Helper()
	id, uuid, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "prompt", Result: "result", ContextID: contextID, ModelID: modelID})
	require.NoError(t, err)
	return id, uuid
}

func storePromptAt(t *testing.T, repo Repository, org UserProjektoveOrganization, contextID int, modelID int, status PromptStatus) (int, string) {
	t.Helper()
	id, uuid, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "prompt", Result: "result", ContextID: contextID, ModelID: modelID, Status: status})
	require.NoError(t, err)
	return id, uuid
}

func TestUser(t *testing.T) {
	repo := newRepository(t)
	ctx := t.Context()
	obj := UserCreate{Email: "user@email.com"}

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

func TestUserModels_CRUD(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	m1 := storeUserModel(t, repo, user, "googleai", "gemini", "g-token")
	m2 := storeUserModel(t, repo, user, "openai", "gpt", "o-token")
	require.NotEqual(t, m1.UUID, m2.UUID)

	models, err := repo.ListUserModels(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, models, 2)

	byUUID := map[string]UserModel{}
	for _, m := range models {
		byUUID[m.UUID] = m
	}
	assert.Equal(t, "gemini", byUUID[m1.UUID].Model)
	assert.Equal(t, "googleai", byUUID[m1.UUID].Provider)
	assert.Equal(t, "g-token", byUUID[m1.UUID].Token)

	require.NoError(t, repo.UpdateUserModel(t.Context(), user.ID, m1.UUID, "new-token"))
	updated, err := repo.GetUserModelByUUID(t.Context(), user.ID, m1.UUID)
	require.NoError(t, err)
	assert.Equal(t, "new-token", updated.Token)

	require.NoError(t, repo.DeleteUserModel(t.Context(), user.ID, m1.UUID))
	_, err = repo.GetUserModelByUUID(t.Context(), user.ID, m1.UUID)
	assert.True(t, errors.Is(err, ErrUserModelNotFound))
}

func TestGetUserModelByModelID(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	other := storeUser(t, repo, "other@email.com")

	m := storeUserModel(t, repo, user, "googleai", "gemini", "g-token")

	got, err := repo.GetUserModelByModelID(t.Context(), user.ID, m.ModelID)
	require.NoError(t, err)
	assert.Equal(t, m.UUID, got.UUID)

	_, err = repo.GetUserModelByModelID(t.Context(), other.ID, m.ModelID)
	assert.True(t, errors.Is(err, ErrUserModelNotFound))
}

func TestUserOrganizations(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	org := storeOrg(t, repo, user)

	orgs, err := repo.ListUserProjektoveOrganizations(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, org.UUID, orgs[0].UUID)
	assert.Equal(t, "token", orgs[0].Token)

	require.NoError(t, repo.UpdateUserOrganizationToken(t.Context(), user.ID, org.UUID, "new-token"))
	got, err := repo.GetUserProjektoveOrganization(t.Context(), org.UUID)
	require.NoError(t, err)
	assert.Equal(t, "new-token", got.Token)

	// other users cannot update this org's token
	other := storeUser(t, repo, "other@email.com")
	err = repo.UpdateUserOrganizationToken(t.Context(), other.ID, org.UUID, "hacked")
	assert.True(t, errors.Is(err, ErrOrganizationNotFound))
}

func TestStoreContext(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	id, uuid, err := repo.StoreContext(t.Context(), user, LLMContextCreate{Name: "c1", Context: "ctx"})
	require.NoError(t, err)
	require.NotZero(t, id)
	require.NotEmpty(t, uuid)

	got, err := repo.GetContext(t.Context(), user, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, uuid, got.UUID)
	assert.Equal(t, "c1", got.Name)
	assert.Equal(t, "ctx", got.Context)

	byUUID, err := repo.GetContextByUUID(t.Context(), user, uuid)
	require.NoError(t, err)
	assert.Equal(t, id, byUUID.ID)
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
	id, _ := storeContext(t, repo, owner, "c1")

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
	id, uuid := storeContext(t, repo, user, "c1")

	err := repo.DeleteContextByUUID(t.Context(), user, uuid)
	require.NoError(t, err)

	_, err = repo.GetContext(t.Context(), user, id)
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestDeleteContext_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	err := repo.DeleteContextByUUID(t.Context(), user, "missing")
	assert.True(t, errors.Is(err, ErrContextNotFound))
}

func TestDeleteContext_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	id, uuid := storeContext(t, repo, owner, "c1")

	err := repo.DeleteContextByUUID(t.Context(), other, uuid)
	assert.True(t, errors.Is(err, ErrContextNotFound))

	_, err = repo.GetContext(t.Context(), owner, id)
	require.NoError(t, err)
}

func TestDeleteContext_InUse(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	id, uuid := storeContext(t, repo, user, "c1")
	storePrompt(t, repo, org, id, modelID)

	err := repo.DeleteContextByUUID(t.Context(), user, uuid)
	assert.True(t, errors.Is(err, ErrContextInUse))

	_, err = repo.GetContext(t.Context(), user, id)
	require.NoError(t, err)
}

func TestListPrompts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")

	id1, uuid1, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "p1", Result: "r1", ContextID: contextID, ModelID: modelID})
	require.NoError(t, err)
	require.NotZero(t, id1)
	require.NotEmpty(t, uuid1)
	id2, uuid2, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "p2", Result: "r2", ContextID: contextID, ModelID: modelID})
	require.NoError(t, err)
	require.NotZero(t, id2)
	require.NotEmpty(t, uuid2)

	prompts, hasMore, err := repo.ListPrompts(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, prompts, 2)
	assert.False(t, hasMore)

	// newest first
	assert.Equal(t, uuid2, prompts[0].UUID)
	assert.Equal(t, uuid1, prompts[1].UUID)

	// created_at is populated on store
	assert.False(t, prompts[0].CreatedAt.IsZero())
}

func TestListPrompts_Pagination(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")

	ids := []int{}
	uuids := []string{}
	for i := 0; i < 3; i++ {
		id, uuid, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "p", ContextID: contextID, ModelID: modelID})
		require.NoError(t, err)
		ids = append(ids, id)
		uuids = append(uuids, uuid)
	}

	page1, hasMore, err := repo.ListPrompts(t.Context(), user, 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, uuids[2], page1[0].UUID)
	assert.Equal(t, uuids[1], page1[1].UUID)

	page2, hasMore, err := repo.ListPrompts(t.Context(), user, 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.False(t, hasMore)
	assert.Equal(t, uuids[0], page2[0].UUID)
}

func TestListPrompts_IssueCounts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")

	promptID, _, err := repo.StorePrompt(t.Context(), org, PromptCreate{Prompt: "p1", ContextID: contextID, ModelID: modelID})
	require.NoError(t, err)

	submitted := newIssueCreate(IssueParentPrompt, promptID)
	submittedID, err := repo.StoreIssue(t.Context(), org, submitted)
	require.NoError(t, err)
	require.NotZero(t, submittedID)

	done := newIssueCreate(IssueParentPrompt, promptID)
	done.Subject = "done"
	doneID, err := repo.StoreIssue(t.Context(), org, done)
	require.NoError(t, err)
	require.NotZero(t, doneID)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), org, IssueParentPrompt, promptID, submittedID, IssueUpdate{
		Subject:      submitted.Subject,
		Description:  submitted.Description,
		ProjectID:    submitted.ProjectID,
		StartDate:    submitted.StartDate,
		DueDate:      submitted.DueDate,
		AssignedToID: submitted.AssignedToID,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))

	prompts, hasMore, err := repo.ListPrompts(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, prompts, 1)
	assert.False(t, hasMore)
	assert.Equal(t, 2, prompts[0].TotalIssues)
	assert.Equal(t, 1, prompts[0].SubmittedIssues)
}

func TestListPrompts_Empty(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	prompts, hasMore, err := repo.ListPrompts(t.Context(), user, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, prompts)
	assert.False(t, hasMore)
}

func storeBatch(t *testing.T, repo Repository, org UserProjektoveOrganization, rawCSV string) (int, string) {
	t.Helper()
	id, uuid, err := repo.StoreBatch(t.Context(), org, rawCSV)
	require.NoError(t, err)
	return id, uuid
}

func TestStoreBatch_GetBatch(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	id, uuid, err := repo.StoreBatch(t.Context(), org, "subject,project")
	require.NoError(t, err)
	require.NotZero(t, id)
	require.NotEmpty(t, uuid)

	byID, err := repo.GetBatch(t.Context(), org, id)
	require.NoError(t, err)
	assert.Equal(t, id, byID.ID)
	assert.Equal(t, uuid, byID.UUID)
	assert.Equal(t, "subject,project", byID.FileContent)
	assert.False(t, byID.CreatedAt.IsZero())

	byUUID, err := repo.GetBatchByUUID(t.Context(), org, uuid)
	require.NoError(t, err)
	assert.Equal(t, id, byUUID.ID)
}

func TestGetBatch_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	_, err := repo.GetBatch(t.Context(), org, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchNotFound))

	_, err = repo.GetBatchByUUID(t.Context(), org, "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchNotFound))
}

func TestGetBatch_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	ownerOrg := storeOrg(t, repo, owner)
	otherOrg := storeOrg(t, repo, other)
	id, uuid := storeBatch(t, repo, ownerOrg, "raw")

	_, err := repo.GetBatch(t.Context(), otherOrg, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchNotFound))

	_, err = repo.GetBatchByUUID(t.Context(), otherOrg, uuid)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchNotFound))
}

func TestListBatches(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	_, uuid1 := storeBatch(t, repo, org, "raw1")
	_, uuid2 := storeBatch(t, repo, org, "raw2")

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, batches, 2)
	assert.False(t, hasMore)

	assert.Equal(t, uuid2, batches[0].UUID)
	assert.Equal(t, uuid1, batches[1].UUID)
	assert.Equal(t, "raw2", batches[0].FileContent)
	assert.False(t, batches[0].CreatedAt.IsZero())
}

func TestListBatches_Pagination(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	uuids := []string{}
	for i := 0; i < 3; i++ {
		_, uuid := storeBatch(t, repo, org, "raw")
		uuids = append(uuids, uuid)
	}

	page1, hasMore, err := repo.ListBatches(t.Context(), user, 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, uuids[2], page1[0].UUID)
	assert.Equal(t, uuids[1], page1[1].UUID)

	page2, hasMore, err := repo.ListBatches(t.Context(), user, 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.False(t, hasMore)
	assert.Equal(t, uuids[0], page2[0].UUID)
}

func TestListBatches_Empty(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, batches)
	assert.False(t, hasMore)
}

func TestListBatches_IsolatedByUser(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	storeBatch(t, repo, storeOrg(t, repo, owner), "raw")

	batches, hasMore, err := repo.ListBatches(t.Context(), other, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, batches)
	assert.False(t, hasMore)
}

func TestListBatches_IssueCounts(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	batchID, _ := storeBatch(t, repo, org, "raw")

	submitted := newIssueCreate(IssueParentBatch, batchID)
	submittedID, err := repo.StoreIssue(t.Context(), org, submitted)
	require.NoError(t, err)
	require.NotZero(t, submittedID)

	created := newIssueCreate(IssueParentBatch, batchID)
	created.Subject = "created"
	createdID, err := repo.StoreIssue(t.Context(), org, created)
	require.NoError(t, err)
	require.NotZero(t, createdID)

	projektoveID := 42
	require.NoError(t, repo.UpdateIssue(t.Context(), org, IssueParentBatch, batchID, submittedID, IssueUpdate{
		Subject:      submitted.Subject,
		Description:  submitted.Description,
		ProjectID:    submitted.ProjectID,
		StartDate:    submitted.StartDate,
		DueDate:      submitted.DueDate,
		AssignedToID: submitted.AssignedToID,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}))

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.False(t, hasMore)
	assert.Equal(t, 2, batches[0].TotalIssues)
	assert.Equal(t, 1, batches[0].SubmittedIssues)
}

func TestListBatches_IssueCounts_ScopedToBatch(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	storeBatch(t, repo, org, "raw")

	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)
	_, err := repo.StoreIssue(t.Context(), org, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	batches, hasMore, err := repo.ListBatches(t.Context(), user, 20, 0)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.False(t, hasMore)
	assert.Equal(t, 0, batches[0].TotalIssues)
	assert.Equal(t, 0, batches[0].SubmittedIssues)
}

func TestUpdateAndListProjects(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}, {ID: 2, Name: "p2", Description: "d2"}}

	err := repo.UpdateProjectsCache(t.Context(), org, projects)
	require.NoError(t, err)

	entry, err := repo.ListProjects(t.Context(), org)
	require.NoError(t, err)
	assert.ElementsMatch(t, projects, entry.Projects)
	assert.False(t, entry.FetchedAt.IsZero())
}

func TestListProjects_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	_, err := repo.ListProjects(t.Context(), org)
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
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	issueID, err := repo.StoreIssue(t.Context(), org, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	require.NotZero(t, issueID)
}

func TestListIssues(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	id1, err := repo.StoreIssue(t.Context(), org, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)
	issue2 := newIssueCreate(IssueParentPrompt, promptID)
	issue2.Subject = "subject2"
	id2, err := repo.StoreIssue(t.Context(), org, issue2)
	require.NoError(t, err)

	issues, err := repo.ListIssues(t.Context(), org, IssueParentPrompt, promptID)
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
	ownerOrg := storeOrg(t, repo, owner)
	otherOrg := storeOrg(t, repo, other)
	modelID := storeUserModel(t, repo, owner, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, owner, "c1")
	promptID, _ := storePrompt(t, repo, ownerOrg, contextID, modelID)

	_, err := repo.ListIssues(t.Context(), otherOrg, IssueParentPrompt, promptID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}

func TestUpdateIssue(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)
	issueID, err := repo.StoreIssue(t.Context(), org, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	projektoveID := 42
	obj := IssueUpdate{
		Subject:      "updated",
		Description:  "updated desc",
		ProjectID:    2,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &projektoveID,
	}
	err = repo.UpdateIssue(t.Context(), org, IssueParentPrompt, promptID, issueID, obj)
	require.NoError(t, err)

	issues, err := repo.ListIssues(t.Context(), org, IssueParentPrompt, promptID)
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
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	err := repo.UpdateIssue(t.Context(), org, IssueParentPrompt, promptID, 999, IssueUpdate{Status: IssueStatusSubmitted})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIssueNotFound))
}

func TestUpdateIssue_ParentNotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	ownerOrg := storeOrg(t, repo, owner)
	otherOrg := storeOrg(t, repo, other)
	modelID := storeUserModel(t, repo, owner, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, owner, "c1")
	promptID, _ := storePrompt(t, repo, ownerOrg, contextID, modelID)

	err := repo.UpdateIssue(t.Context(), otherOrg, IssueParentPrompt, promptID, 1, IssueUpdate{Status: IssueStatusSubmitted})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}

func TestGetPrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, promptUUID := storePrompt(t, repo, org, contextID, modelID)

	prompt, err := repo.GetPrompt(t.Context(), org, promptID)
	require.NoError(t, err)
	assert.Equal(t, promptID, prompt.ID)
	assert.Equal(t, promptUUID, prompt.UUID)
	assert.Equal(t, "prompt", prompt.Prompt)
	assert.Equal(t, "result", prompt.Result)
	assert.Equal(t, "c1", prompt.Context.Name)

	byUUID, err := repo.GetPromptByUUID(t.Context(), org, promptUUID)
	require.NoError(t, err)
	assert.Equal(t, promptID, byUUID.ID)
}

func TestGetPrompt_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	_, err := repo.GetPrompt(t.Context(), org, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestGetPrompt_NotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	ownerOrg := storeOrg(t, repo, owner)
	otherOrg := storeOrg(t, repo, other)
	modelID := storeUserModel(t, repo, owner, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, owner, "c1")
	promptID, promptUUID := storePrompt(t, repo, ownerOrg, contextID, modelID)

	_, err := repo.GetPrompt(t.Context(), otherOrg, promptID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))

	_, err = repo.GetPromptByUUID(t.Context(), otherOrg, promptUUID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestGetUserByID(t *testing.T) {
	repo := newRepository(t)
	obj := UserCreate{Email: "user@email.com"}

	id, err := repo.StoreUser(t.Context(), obj)
	require.NoError(t, err)

	user, err := repo.GetUserByID(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, id, user.ID)
	assert.Equal(t, obj.Email, user.Email)
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
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	err := repo.SetPromptProcessing(t.Context(), org, promptID, "full prompt")
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), org, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusProcessing, prompt.Status)
	assert.Equal(t, "full prompt", prompt.Prompt)
}

func TestSetPromptProcessing_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	err := repo.SetPromptProcessing(t.Context(), org, 999, "prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestCompletePrompt(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	err := repo.CompletePrompt(t.Context(), org, promptID, PromptComplete{
		Status: PromptStatusDone,
		Prompt: "p",
		Result: "r",
	})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), org, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusDone, prompt.Status)
	assert.Equal(t, "p", prompt.Prompt)
	assert.Equal(t, "r", prompt.Result)
	assert.Empty(t, prompt.Error)
}

func TestCompletePrompt_Error(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	err := repo.CompletePrompt(t.Context(), org, promptID, PromptComplete{
		Status: PromptStatusError,
		Error:  "boom",
	})
	require.NoError(t, err)

	prompt, err := repo.GetPrompt(t.Context(), org, promptID)
	require.NoError(t, err)
	assert.Equal(t, PromptStatusError, prompt.Status)
	assert.Equal(t, "boom", prompt.Error)
}

func TestCompletePrompt_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)

	err := repo.CompletePrompt(t.Context(), org, 999, PromptComplete{Status: PromptStatusDone})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound))
}

func TestEnqueueTask(t *testing.T) {
	repo := newRepository(t)

	payload := InferenceJob{UserProjektoveOrganizationID: 1, PromptID: 2}
	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: payload})
	require.NoError(t, err)
	require.NotZero(t, id)

	task, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, id, task.ID)
	assert.Equal(t, TaskTypeInference, task.Type)
	assert.Equal(t, TaskStatusProcessing, task.Status)
	assert.Equal(t, payload, task.Payload)
}

func TestClaimTask_Empty(t *testing.T) {
	repo := newRepository(t)

	_, ok, err := repo.ClaimTask(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestClaimTask_Exclusive(t *testing.T) {
	repo := newRepository(t)

	id1, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 1, PromptID: 1}})
	require.NoError(t, err)
	id2, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 2, PromptID: 2}})
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

	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 1, PromptID: 1}})
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

	id, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 1, PromptID: 1}})
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

	doneID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 1, PromptID: 1}})
	require.NoError(t, err)
	orphanID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 2, PromptID: 2}})
	require.NoError(t, err)
	failedID, err := repo.EnqueueTask(t.Context(), TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: 3, PromptID: 3}})
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
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)
	issueID, err := repo.StoreIssue(t.Context(), org, newIssueCreate(IssueParentPrompt, promptID))
	require.NoError(t, err)

	issue, err := repo.GetIssue(t.Context(), org, IssueParentPrompt, promptID, issueID)
	require.NoError(t, err)
	assert.Equal(t, issueID, issue.ID)
	assert.Equal(t, "subject", issue.Subject)
	assert.Equal(t, IssueStatusCreated, issue.Status)
}

func TestGetIssue_NotFound(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "user@email.com")
	org := storeOrg(t, repo, user)
	modelID := storeUserModel(t, repo, user, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, user, "c1")
	promptID, _ := storePrompt(t, repo, org, contextID, modelID)

	_, err := repo.GetIssue(t.Context(), org, IssueParentPrompt, promptID, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIssueNotFound))
}

func TestGetIssue_ParentNotOwned(t *testing.T) {
	repo := newRepository(t)
	owner := storeUser(t, repo, "owner@email.com")
	other := storeUser(t, repo, "other@email.com")
	ownerOrg := storeOrg(t, repo, owner)
	otherOrg := storeOrg(t, repo, other)
	modelID := storeUserModel(t, repo, owner, "googleai", "gemini", "g-token").ModelID
	contextID, _ := storeContext(t, repo, owner, "c1")
	promptID, _ := storePrompt(t, repo, ownerOrg, contextID, modelID)

	_, err := repo.GetIssue(t.Context(), otherOrg, IssueParentPrompt, promptID, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrParentDoesNotBelongToUser))
}

func TestStoreProjektoveOrganization(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	require.NotZero(t, id)

	orgs, err := repo.ListProjektoveOrganizations(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, id, orgs[0].ID)
	assert.Equal(t, "acme", orgs[0].Name)
	assert.Equal(t, "https://api.example.com", orgs[0].APIURL)
	assert.Equal(t, "https://app.example.com", orgs[0].BrowserURL)
}

func TestGetProjektoveOrganization(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	got, err := repo.GetProjektoveOrganization(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "acme", got.Name)

	_, err = repo.GetProjektoveOrganization(t.Context(), 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOrganizationNotFound))
}

func TestGetProjektoveOrganizationByName(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	got, err := repo.GetProjektoveOrganizationByName(t.Context(), "acme")
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "acme", got.Name)

	_, err = repo.GetProjektoveOrganizationByName(t.Context(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOrganizationNotFound))
}

func TestStoreUserProjectoveOrganization(t *testing.T) {
	repo := newRepository(t)

	user := storeUser(t, repo, "user@example.com")

	orgID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	uuid, err := repo.StoreUserProjectoveOrganization(t.Context(), user.ID, orgID, "tok1")
	require.NoError(t, err)
	require.NotEmpty(t, uuid)

	orgs, err := repo.ListUserProjektoveOrganizations(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, orgID, orgs[0].OrganizationID)
	assert.Equal(t, "tok1", orgs[0].Token)
	assert.Equal(t, uuid, orgs[0].UUID)

	uuid2, err := repo.StoreUserProjectoveOrganization(t.Context(), user.ID, orgID, "tok2")
	require.NoError(t, err)
	assert.Equal(t, uuid, uuid2)

	orgs, err = repo.ListUserProjektoveOrganizations(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "tok2", orgs[0].Token)
}

func TestUpdateProjektoveOrganization(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	require.NoError(t, repo.UpdateProjektoveOrganization(t.Context(), id, "acme2", "https://api2.example.com", "https://app2.example.com"))

	got, err := repo.GetProjektoveOrganization(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, "acme2", got.Name)
	assert.Equal(t, "https://api2.example.com", got.APIURL)
	assert.Equal(t, "https://app2.example.com", got.BrowserURL)

	err = repo.UpdateProjektoveOrganization(t.Context(), 999, "nope", "nope", "nope")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOrganizationNotFound))
}

func TestStoreOrganizationUser(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	require.NoError(t, repo.StoreOrganizationUser(t.Context(), id, "jane", 42))
	require.NoError(t, repo.StoreOrganizationUser(t.Context(), id, "john", 43))

	users, err := repo.ListOrganizationUsers(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, "jane", users[0].Name)
	assert.Equal(t, 42, users[0].ProjektoveID)
	assert.Equal(t, "john", users[1].Name)
}

func TestDeleteOrganizationUser(t *testing.T) {
	repo := newRepository(t)

	id, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	require.NoError(t, repo.StoreOrganizationUser(t.Context(), id, "jane", 42))
	users, err := repo.ListOrganizationUsers(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, users, 1)

	require.NoError(t, repo.DeleteOrganizationUser(t.Context(), id, users[0].ID))

	users, err = repo.ListOrganizationUsers(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, users)

	err = repo.DeleteOrganizationUser(t.Context(), 999, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOrganizationNotFound))
}

func TestListModels(t *testing.T) {
	repo := newRepository(t)

	providerID1, err := repo.GetOrCreateProvider(t.Context(), "openai")
	require.NoError(t, err)
	providerID2, err := repo.GetOrCreateProvider(t.Context(), "googleai")
	require.NoError(t, err)

	_, _, err = repo.GetOrCreateModel(t.Context(), providerID1, "gpt-4o")
	require.NoError(t, err)
	_, _, err = repo.GetOrCreateModel(t.Context(), providerID2, "gemini")
	require.NoError(t, err)

	models, err := repo.ListModels(t.Context())
	require.NoError(t, err)
	require.Len(t, models, 2)

	names := map[string]bool{}
	for _, m := range models {
		names[m.Provider+" / "+m.Model] = true
	}
	assert.True(t, names["openai / gpt-4o"])
	assert.True(t, names["googleai / gemini"])
}
