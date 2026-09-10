package projektovemeeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type Controller struct {
	TxProvider     *TxProvider
	Repository     Repository
	Projektove     Projektove
	NewLLMProvider func(provider LLMProvider, model string, token string) (LLM, error)
}

func (c Controller) RunInference(ctx context.Context, job InferenceJob) error {
	org, err := c.Repository.GetUserProjektoveOrganizationByID(ctx, job.UserProjektoveOrganizationID)
	if err != nil {
		return fmt.Errorf("when loading organization %d: %w", job.UserProjektoveOrganizationID, err)
	}

	prompt, err := c.Repository.GetPrompt(ctx, org, job.PromptID)
	if err != nil {
		return fmt.Errorf("when loading prompt %d: %w", job.PromptID, err)
	}
	if !prompt.Status.IsPending() {
		return nil
	}

	projects, err := c.Projektove.GetProjects(ctx, org.Token, org.APIURL)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{}, fmt.Errorf("when listing projects: %w", err))
		return err
	}

	userModel, err := c.Repository.GetUserModelByModelID(ctx, org.UserID, prompt.ModelID)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{}, err)
		return err
	}

	llm, err := c.NewLLMProvider(LLMProvider(userModel.Provider), userModel.Model, userModel.Token)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{}, fmt.Errorf("when setting up llm: %w", err))
		return err
	}

	orgUsers, err := c.repositoryOrganizationUsers(ctx, org)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{}, err)
		return err
	}

	promptText, err := buildInferencePrompt(projects, orgUsers, prompt.Context.Context, prompt.FileContent)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{}, err)
		return err
	}

	if err := c.Repository.SetPromptProcessing(ctx, org, job.PromptID, promptText); err != nil {
		return fmt.Errorf("when marking prompt as processing: %w", err)
	}

	slog.Debug("Inferring...", "prompt_id", job.PromptID)
	inference, err := llm.Infer(ctx, promptText)
	slog.Debug("Inference done", "prompt_id", job.PromptID)
	if err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{Prompt: promptText, Result: inference}, err)
		return err
	}

	objs := []IssueCreate{}
	if err := json.Unmarshal([]byte(inference), &objs); err != nil {
		c.failPrompt(ctx, org, job.PromptID, PromptComplete{Prompt: promptText, Result: inference}, fmt.Errorf("when unmarshalling response: %w", err))
		return err
	}

	if err := c.TxProvider.Transact(func(repo Repository) error {
		for _, iss := range objs {
			iss.Parent = IssueParentPrompt
			iss.ParentID = job.PromptID
			if _, err := repo.StoreIssue(ctx, org, iss); err != nil {
				return fmt.Errorf("when storing issue: %w", err)
			}
		}
		if err := repo.CompletePrompt(ctx, org, job.PromptID, PromptComplete{Prompt: promptText, Result: inference, Status: PromptStatusDone}); err != nil {
			return fmt.Errorf("when marking prompt as done: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("when storing inference results: %w", err)
	}

	return nil
}

func (c Controller) repositoryOrganizationUsers(ctx context.Context, org UserProjektoveOrganization) ([]ProjektoveUser, error) {
	orgUsers, err := c.Repository.ListOrganizationUsers(ctx, org.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("when listing organization users: %w", err)
	}
	users := make([]ProjektoveUser, 0, len(orgUsers))
	for _, u := range orgUsers {
		users = append(users, ProjektoveUser{ID: u.ProjektoveID, Name: u.Name})
	}
	return users, nil
}

func (c Controller) failPrompt(ctx context.Context, org UserProjektoveOrganization, promptID int, partial PromptComplete, cause error) {
	slog.Error("Inference failed", "prompt_id", promptID, "err", cause.Error())
	if err := c.Repository.CompletePrompt(ctx, org, promptID, PromptComplete{
		Prompt: partial.Prompt,
		Result: partial.Result,
		Error:  cause.Error(),
		Status: PromptStatusError,
	}); err != nil {
		slog.Error("Failed to mark prompt as errored", "prompt_id", promptID, "err", err.Error())
	}
}

func buildInferencePrompt(projects []ProjektoveProject, users []ProjektoveUser, contextText, meeting string) (string, error) {
	projectsMarshalled, err := json.Marshal(projects)
	if err != nil {
		return "", fmt.Errorf("when encoding projects: %w", err)
	}
	usersMarshalled, err := json.Marshal(users)
	if err != nil {
		return "", fmt.Errorf("when encoding users: %w", err)
	}

	prompt := fmt.Sprintf(`Given these meeting notes please create in structured
	json format an output with a list of task that can be uploaded to an issue tracker.

	Users:
	%s

	Projects:
	%s

	Context:
	%s

	Output JSON Schema:
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Projektove Issue",
  "type": "object",
  "properties": {
	"subject": {
	  "type": "string"
	},
	"description": {
	  "type": "string"
	},
	"project_id": {
	  "type": "integer",
	  "description": "id of the project to assign the task to. Please use the Projects object above as well as any info in the Context section. If not sure leave it empty"
	},
	"start_date": {
	  "type": "string",
	  "format": "date-time",
	  "description": "use RFC3339"
	},
	"due_date": {
	  "type": "string",
	  "format": "date-time",
	  "description": "use RFC3339"
	},
	"assigned_to_id": {
	  "type": "number",
	  "description": "id of the person to assign the issue to, inferred from the Users array above. Leave it empty if you cannot determine the person"
	},
  },
  "required": ["subject"],
  "additionalProperties": true
}

	Meeting notes:
	%s
	`, usersMarshalled, projectsMarshalled, contextText, meeting)

	return prompt, nil
}

func (c Controller) ListOrgUsers(ctx context.Context, org UserProjektoveOrganization) ([]ProjektoveUser, error) {
	return c.repositoryOrganizationUsers(ctx, org)
}

func (c Controller) GetIssueOrg(ctx context.Context, user User, issueUUID string) (UserProjektoveOrganization, error) {
	return c.orgForIssue(ctx, user, issueUUID)
}

func (c Controller) GetUserProfile(ctx context.Context, user User) (UserProfileView, error) {
	profile := UserProfileView{
		Email: user.Email,
	}

	models, err := c.Repository.ListUserModels(ctx, user.ID)
	if err != nil {
		return UserProfileView{}, fmt.Errorf("when listing user models: %w", err)
	}
	for _, m := range models {
		profile.Models = append(profile.Models, UserModelView{ID: m.UUID, Provider: m.Provider, Model: m.Model, Token: m.Token})
	}

	availableModels, err := c.Repository.ListModels(ctx)
	if err != nil {
		return UserProfileView{}, fmt.Errorf("when listing available models: %w", err)
	}
	profile.AvailableModels = availableModels

	availableOrganizations, err := c.Repository.ListProjektoveOrganizations(ctx)
	if err != nil {
		return UserProfileView{}, fmt.Errorf("when listing available organizations: %w", err)
	}
	profile.AvailableOrganizations = availableOrganizations

	orgs, err := c.Repository.ListUserProjektoveOrganizations(ctx, user.ID)
	if err != nil {
		return UserProfileView{}, fmt.Errorf("when listing user organizations: %w", err)
	}
	for _, o := range orgs {
		profile.Organizations = append(profile.Organizations, UserOrgView{ID: o.UUID, Name: o.OrgName, Token: o.Token})
	}

	contexts, err := c.Repository.ListContexts(ctx, user)
	if err != nil {
		return UserProfileView{}, fmt.Errorf("when listing contexts: %w", err)
	}
	for _, c := range contexts {
		profile.Contexts = append(profile.Contexts, ContextView{ID: c.UUID, Name: c.Name, Context: c.Context})
	}

	return profile, nil
}

func (c Controller) UpdateUser(ctx context.Context, user User, v UserUpdateView) error {
	current, err := c.Repository.ListUserModels(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("when listing user models: %w", err)
	}

	submitted := make(map[string]bool)
	for _, m := range v.Models {
		if m.ID != "" {
			submitted[m.ID] = true
		}
	}
	for _, m := range current {
		if !submitted[m.UUID] {
			if err := c.Repository.DeleteUserModel(ctx, user.ID, m.UUID); err != nil {
				return fmt.Errorf("when deleting user model: %w", err)
			}
		}
	}

	for _, m := range v.Models {
		if m.ID == "" {
			if m.Provider == "" || m.Model == "" {
				continue
			}
			if err := c.AddUserModel(ctx, user, m.Provider, m.Model, m.Token); err != nil {
				return err
			}
			continue
		}
		if err := c.Repository.UpdateUserModel(ctx, user.ID, m.ID, m.Token); err != nil {
			return fmt.Errorf("when updating user model: %w", err)
		}
	}

	for _, o := range v.Organizations {
		if o.ID == "" {
			if o.OrganizationName == "" {
				continue
			}
			org, err := c.Repository.GetProjektoveOrganizationByName(ctx, o.OrganizationName)
			if err != nil {
				if errors.Is(err, ErrOrganizationNotFound) {
					continue
				}
				return fmt.Errorf("when getting organization by name: %w", err)
			}
			if _, err := c.Repository.StoreUserProjectoveOrganization(ctx, user.ID, org.ID, o.Token); err != nil {
				return fmt.Errorf("when storing user organization: %w", err)
			}
			continue
		}
		if err := c.Repository.UpdateUserOrganizationToken(ctx, user.ID, o.ID, o.Token); err != nil {
			return fmt.Errorf("when updating organization token: %w", err)
		}
	}

	return nil
}

func (c Controller) ResolveProjectoveOrganization(ctx context.Context, name string) (ProjektoveOrganization, error) {
	org, err := c.Repository.GetProjektoveOrganizationByName(ctx, name)
	if err != nil {
		return ProjektoveOrganization{}, fmt.Errorf("when resolving organization by name: %w", err)
	}
	return org, nil
}

func (c Controller) AddUserModel(ctx context.Context, user User, provider, model, token string) error {
	providerID, err := c.Repository.GetOrCreateProvider(ctx, provider)
	if err != nil {
		return fmt.Errorf("when getting provider: %w", err)
	}

	modelID, _, err := c.Repository.GetOrCreateModel(ctx, providerID, model)
	if err != nil {
		return fmt.Errorf("when getting model: %w", err)
	}

	if _, _, err := c.Repository.StoreUserModel(ctx, user.ID, modelID, token); err != nil {
		return fmt.Errorf("when storing user model: %w", err)
	}

	return nil
}

func (c Controller) DeleteUserModel(ctx context.Context, user User, uuid string) error {
	if err := c.Repository.DeleteUserModel(ctx, user.ID, uuid); err != nil {
		return err
	}
	return nil
}

func (c Controller) StoreContext(ctx context.Context, user User, v LLMContextCreate) (ContextView, error) {
	_, uuid, err := c.Repository.StoreContext(ctx, user, v)
	if err != nil {
		return ContextView{}, fmt.Errorf("when storing context: %w", err)
	}

	return ContextView{ID: uuid, Name: v.Name, Context: v.Context}, nil
}

func (c Controller) ResolveContext(ctx context.Context, user User, uuid string) (LLMContext, error) {
	cx, err := c.Repository.GetContextByUUID(ctx, user, uuid)
	if err != nil {
		return LLMContext{}, err
	}
	return cx, nil
}

func (c Controller) DeleteContext(ctx context.Context, user User, uuid string) error {
	if err := c.Repository.DeleteContextByUUID(ctx, user, uuid); err != nil {
		return err
	}

	return nil
}

func (c Controller) CreatePrompt(ctx context.Context, user User, userModelUUID, orgUUID string, contextID int, meeting string) (string, error) {
	org, err := c.Repository.GetUserProjektoveOrganization(ctx, orgUUID)
	if err != nil {
		return "", err
	}

	userModel, err := c.Repository.GetUserModelByUUID(ctx, user.ID, userModelUUID)
	if err != nil {
		return "", ErrUserModelNotFound
	}

	if strings.TrimSpace(org.Token) == "" {
		return "", ErrProjektoveTokenNotConfigured
	}

	promptUUID := ""
	if err := c.TxProvider.Transact(func(repo Repository) error {
		id, uuid, err := repo.StorePrompt(ctx, org, PromptCreate{
			ContextID:                    contextID,
			Status:                       PromptStatusCreated,
			ModelID:                      userModel.ModelID,
			FileContent:                  meeting,
			UserProjektoveOrganizationID: org.ID,
		})
		if err != nil {
			return fmt.Errorf("when storing prompt: %w", err)
		}
		promptUUID = uuid

		if _, err := repo.EnqueueTask(ctx, TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserProjektoveOrganizationID: org.ID, PromptID: id}}); err != nil {
			return fmt.Errorf("when enqueuing inference task: %w", err)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("when creating prompt: %w", err)
	}

	return promptUUID, nil
}

func (c Controller) ListContexts(ctx context.Context, user User) ([]ContextView, error) {
	contexts, err := c.Repository.ListContexts(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("when listing contexts: %w", err)
	}

	views := make([]ContextView, 0, len(contexts))
	for _, c := range contexts {
		views = append(views, ContextView{ID: c.UUID, Name: c.Name, Context: c.Context})
	}

	return views, nil
}

func (c Controller) ListProjects(ctx context.Context, org UserProjektoveOrganization) ([]ProjectOptionView, error) {
	projects, err := c.Projektove.GetProjects(ctx, org.Token, org.APIURL)
	if err != nil {
		return nil, fmt.Errorf("when listing projects: %w", err)
	}

	views := make([]ProjectOptionView, 0, len(projects))
	for _, p := range projects {
		views = append(views, ProjectOptionView{ID: p.ID, Name: p.Name})
	}

	return views, nil
}

func (c Controller) ListPrompts(ctx context.Context, user User, limit, offset int) (PromptListView, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	prompts, hasMore, err := c.Repository.ListPrompts(ctx, user, limit, offset)
	if err != nil {
		return PromptListView{}, fmt.Errorf("when listing prompts: %w", err)
	}

	items := make([]PromptListItem, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, PromptListItem{
			ID:              p.UUID,
			ContextName:     p.Context.Name,
			Status:          p.Status,
			CreatedAt:       p.CreatedAt,
			TotalIssues:     p.TotalIssues,
			SubmittedIssues: p.SubmittedIssues,
		})
	}

	return PromptListView{
		Items:      items,
		HasMore:    hasMore,
		NextOffset: offset + len(items),
	}, nil
}

func (c Controller) GetPrompt(ctx context.Context, user User, uuid string) (PromptView, error) {
	org, err := c.orgForPrompt(ctx, user, uuid)
	if err != nil {
		return PromptView{}, err
	}

	prompt, err := c.Repository.GetPromptByUUID(ctx, org, uuid)
	if err != nil {
		return PromptView{}, err
	}
	return c.buildPromptView(ctx, user, org, prompt)
}

// GetIssueViewByUUID resolves an issue by its uuid and returns a single issue
// view, regardless of its parent (prompt or batch). Used to re-render an
// issue card after update/submit.
func (c Controller) GetIssueViewByUUID(ctx context.Context, user User, issueUUID string) (IssueView, error) {
	org, err := c.orgForIssue(ctx, user, issueUUID)
	if err != nil {
		return IssueView{}, err
	}

	iss, err := c.Repository.GetIssueByUUID(ctx, org, issueUUID)
	if err != nil {
		return IssueView{}, err
	}
	return toIssueView(iss), nil
}

func (c Controller) buildPromptView(ctx context.Context, user User, org UserProjektoveOrganization, prompt Prompt) (PromptView, error) {
	issues, err := c.Repository.ListIssues(ctx, org, IssueParentPrompt, prompt.ID)
	if err != nil {
		return PromptView{}, fmt.Errorf("when listing issues: %w", err)
	}

	view := PromptView{
		ID:          prompt.UUID,
		Prompt:      prompt.Prompt,
		Result:      prompt.Result,
		Error:       prompt.Error,
		ContextName: prompt.Context.Name,
		Status:      prompt.Status,
		Issues:      toIssueViews(issues),
		Model:       prompt.Provider + " / " + prompt.Model,
		OrgID:       org.UUID,
	}

	return view, nil
}

func (c Controller) orgForPrompt(ctx context.Context, user User, uuid string) (UserProjektoveOrganization, error) {
	orgs, err := c.Repository.ListUserProjektoveOrganizations(ctx, user.ID)
	if err != nil {
		return UserProjektoveOrganization{}, err
	}
	for _, o := range orgs {
		if _, err := c.Repository.GetPromptByUUID(ctx, o, uuid); err == nil {
			return o, nil
		}
	}
	return UserProjektoveOrganization{}, ErrPromptNotFound
}

func (c Controller) orgForBatch(ctx context.Context, user User, uuid string) (UserProjektoveOrganization, error) {
	orgs, err := c.Repository.ListUserProjektoveOrganizations(ctx, user.ID)
	if err != nil {
		return UserProjektoveOrganization{}, err
	}
	for _, o := range orgs {
		if _, err := c.Repository.GetBatchByUUID(ctx, o, uuid); err == nil {
			return o, nil
		}
	}
	return UserProjektoveOrganization{}, ErrBatchNotFound
}

func (c Controller) orgForIssue(ctx context.Context, user User, issueUUID string) (UserProjektoveOrganization, error) {
	orgs, err := c.Repository.ListUserProjektoveOrganizations(ctx, user.ID)
	if err != nil {
		return UserProjektoveOrganization{}, err
	}
	for _, o := range orgs {
		if _, err := c.Repository.GetIssueByUUID(ctx, o, issueUUID); err == nil {
			return o, nil
		}
	}
	return UserProjektoveOrganization{}, ErrIssueNotFound
}

func toIssueViews(issues []Issue) []IssueView {
	views := make([]IssueView, 0, len(issues))
	for _, iss := range issues {
		views = append(views, toIssueView(iss))
	}
	return views
}

func toIssueView(iss Issue) IssueView {
	return IssueView{
		ID:           iss.UUID,
		Subject:      iss.Subject,
		Description:  iss.Description,
		ProjectID:    iss.ProjectID,
		StartDate:    iss.StartDate,
		DueDate:      iss.DueDate,
		AssignedToID: iss.AssignedToID,
		ProjektoveID: iss.ProjektoveID,
		Status:       iss.Status,
		Editable:     isEditable(iss.Status),
	}
}

func (c Controller) CreateBatch(ctx context.Context, user User, orgUserProjektoveOrgUUID, rawCSV string) (string, error) {
	org, err := c.Repository.GetUserProjektoveOrganization(ctx, orgUserProjektoveOrgUUID)
	if err != nil {
		return "", err
	}

	orgUsers, err := c.repositoryOrganizationUsers(ctx, org)
	if err != nil {
		return "", err
	}

	projects, err := c.Projektove.GetProjects(ctx, org.Token, org.APIURL)
	if err != nil {
		return "", fmt.Errorf("when listing projects: %w", err)
	}

	objs, err := parseBatchCSV(rawCSV, projects, orgUsers)
	if err != nil {
		return "", err
	}

	batchUUID := ""
	if err := c.TxProvider.Transact(func(repo Repository) error {
		id, uuid, err := repo.StoreBatch(ctx, org, rawCSV)
		if err != nil {
			return fmt.Errorf("when storing batch: %w", err)
		}
		batchUUID = uuid

		for _, iss := range objs {
			iss.Parent = IssueParentBatch
			iss.ParentID = id
			if _, err := repo.StoreIssue(ctx, org, iss); err != nil {
				return fmt.Errorf("when storing issue: %w", err)
			}
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("when creating batch: %w", err)
	}

	return batchUUID, nil
}

func (c Controller) ListBatches(ctx context.Context, user User, limit, offset int) (BatchListView, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	batches, hasMore, err := c.Repository.ListBatches(ctx, user, limit, offset)
	if err != nil {
		return BatchListView{}, fmt.Errorf("when listing batches: %w", err)
	}

	items := make([]BatchListItem, 0, len(batches))
	for _, b := range batches {
		items = append(items, BatchListItem{
			ID:              b.UUID,
			CreatedAt:       b.CreatedAt,
			TotalIssues:     b.TotalIssues,
			SubmittedIssues: b.SubmittedIssues,
		})
	}

	return BatchListView{
		Items:      items,
		HasMore:    hasMore,
		NextOffset: offset + len(items),
	}, nil
}

func (c Controller) GetBatch(ctx context.Context, user User, uuid string) (BatchView, error) {
	org, err := c.orgForBatch(ctx, user, uuid)
	if err != nil {
		return BatchView{}, err
	}

	batch, err := c.Repository.GetBatchByUUID(ctx, org, uuid)
	if err != nil {
		return BatchView{}, err
	}
	return c.buildBatchView(ctx, user, org, batch)
}

func (c Controller) buildBatchView(ctx context.Context, user User, org UserProjektoveOrganization, batch Batch) (BatchView, error) {
	issues, err := c.Repository.ListIssues(ctx, org, IssueParentBatch, batch.ID)
	if err != nil {
		return BatchView{}, fmt.Errorf("when listing issues: %w", err)
	}

	return BatchView{
		ID:          batch.UUID,
		FileContent: batch.FileContent,
		CreatedAt:   batch.CreatedAt,
		Issues:      toIssueViews(issues),
		OrgID:       org.UUID,
	}, nil
}

func (c Controller) UpdateIssue(ctx context.Context, user User, issueUUID string, v IssueUpdateView) error {
	org, err := c.orgForIssue(ctx, user, issueUUID)
	if err != nil {
		return err
	}

	iss, err := c.Repository.GetIssueByUUID(ctx, org, issueUUID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
	}
	if iss.Status == IssueStatusIgnored {
		return ErrIssueIgnored
	}

	obj := IssueUpdate{
		Subject:      v.Subject,
		Description:  v.Description,
		ProjectID:    v.ProjectID,
		StartDate:    v.StartDate,
		DueDate:      v.DueDate,
		AssignedToID: v.AssignedToID,
		Status:       iss.Status,
		ProjektoveID: iss.ProjektoveID,
	}

	if err := c.Repository.UpdateIssue(ctx, org, iss.Parent, iss.ParentID, iss.ID, obj); err != nil {
		return fmt.Errorf("when updating issue: %w", err)
	}

	return nil
}

func (c Controller) SubmitIssue(ctx context.Context, user User, issueUUID string) error {
	org, err := c.orgForIssue(ctx, user, issueUUID)
	if err != nil {
		return err
	}

	iss, err := c.Repository.GetIssueByUUID(ctx, org, issueUUID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
	}
	if iss.Status == IssueStatusIgnored {
		return ErrIssueIgnored
	}

	var missing []string
	if iss.Subject == "" {
		missing = append(missing, "subject")
	}
	if iss.ProjectID == 0 {
		missing = append(missing, "project")
	}
	if iss.AssignedToID == 0 {
		missing = append(missing, "assignee")
	}
	if iss.StartDate.IsZero() {
		missing = append(missing, "start date")
	}
	if iss.DueDate.IsZero() {
		missing = append(missing, "due date")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrIssueIncomplete, strings.Join(missing, ", "))
	}

	obj := ProjektoveIssueCreate{
		Subject:      iss.Subject,
		Description:  iss.Description,
		ProjectID:    iss.ProjectID,
		StartDate:    iss.StartDate,
		DueDate:      iss.DueDate,
		AssignedToID: iss.AssignedToID,
	}

	created, err := c.Projektove.CreateIssue(ctx, org.Token, org.APIURL, obj)
	if err != nil {
		failStatus := IssueStatusSubmitFailed
		if updateErr := c.Repository.UpdateIssue(ctx, org, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
			Subject:      iss.Subject,
			Description:  iss.Description,
			ProjectID:    iss.ProjectID,
			StartDate:    iss.StartDate,
			DueDate:      iss.DueDate,
			AssignedToID: iss.AssignedToID,
			Status:       failStatus,
			ProjektoveID: iss.ProjektoveID,
		}); updateErr != nil {
			return fmt.Errorf("when creating issue: %v; when marking as failed: %w", err, updateErr)
		}
		return fmt.Errorf("when creating issue: %w", err)
	}

	if err := c.Repository.UpdateIssue(ctx, org, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
		Subject:      iss.Subject,
		Description:  iss.Description,
		ProjectID:    iss.ProjectID,
		StartDate:    iss.StartDate,
		DueDate:      iss.DueDate,
		AssignedToID: iss.AssignedToID,
		Status:       IssueStatusSubmitted,
		ProjektoveID: &created.ID,
	}); err != nil {
		return fmt.Errorf("when marking issue as submitted: %w", err)
	}

	return nil
}

func isSubmitted(status IssueStatus) bool {
	return status == IssueStatusSubmitted
}

func isEditable(status IssueStatus) bool {
	return status == IssueStatusCreated || status == IssueStatusSubmitFailed
}

func (c Controller) IgnoreIssue(ctx context.Context, user User, issueUUID string) error {
	org, err := c.orgForIssue(ctx, user, issueUUID)
	if err != nil {
		return err
	}

	iss, err := c.Repository.GetIssueByUUID(ctx, org, issueUUID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
	}
	if iss.Status == IssueStatusIgnored {
		return nil
	}

	if err := c.Repository.UpdateIssue(ctx, org, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
		Subject:      iss.Subject,
		Description:  iss.Description,
		ProjectID:    iss.ProjectID,
		StartDate:    iss.StartDate,
		DueDate:      iss.DueDate,
		AssignedToID: iss.AssignedToID,
		Status:       IssueStatusIgnored,
		ProjektoveID: nil,
	}); err != nil {
		return fmt.Errorf("when ignoring issue: %w", err)
	}

	return nil
}

func (c Controller) ListAdminOrganizations(ctx context.Context) ([]AdminOrganizationView, error) {
	orgs, err := c.Repository.ListProjektoveOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("when listing organizations: %w", err)
	}

	views := make([]AdminOrganizationView, 0, len(orgs))
	for _, o := range orgs {
		users, err := c.Repository.ListOrganizationUsers(ctx, o.ID)
		if err != nil {
			return nil, fmt.Errorf("when listing organization users: %w", err)
		}
		views = append(views, AdminOrganizationView{ID: o.UUID, Name: o.Name, APIURL: o.APIURL, BrowserURL: o.BrowserURL, Users: users})
	}

	return views, nil
}

func (c Controller) GetAdminOrganization(ctx context.Context, uuid string) (AdminOrganizationView, error) {
	o, err := c.Repository.GetProjektoveOrganizationByUUID(ctx, uuid)
	if err != nil {
		return AdminOrganizationView{}, err
	}

	users, err := c.Repository.ListOrganizationUsers(ctx, o.ID)
	if err != nil {
		return AdminOrganizationView{}, fmt.Errorf("when listing organization users: %w", err)
	}

	return AdminOrganizationView{ID: o.UUID, Name: o.Name, APIURL: o.APIURL, BrowserURL: o.BrowserURL, Users: users}, nil
}

func (c Controller) CreateOrganization(ctx context.Context, name, apiURL, browserURL string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(apiURL) == "" || strings.TrimSpace(browserURL) == "" {
		return "", fmt.Errorf("name, api url and browser url are required: %w", ErrInvalidArgument)
	}

	_, uuid, err := c.Repository.StoreProjektoveOrganization(ctx, name, apiURL, browserURL)
	if err != nil {
		return "", fmt.Errorf("when storing organization: %w", err)
	}

	return uuid, nil
}

func (c Controller) UpdateOrganization(ctx context.Context, uuid string, name, apiURL, browserURL string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(apiURL) == "" || strings.TrimSpace(browserURL) == "" {
		return fmt.Errorf("name, api url and browser url are required: %w", ErrInvalidArgument)
	}

	if err := c.Repository.UpdateProjektoveOrganization(ctx, uuid, name, apiURL, browserURL); err != nil {
		return fmt.Errorf("when updating organization: %w", err)
	}

	return nil
}

func (c Controller) AddOrganizationUser(ctx context.Context, orgUUID string, name string, projektoveID int) error {
	if strings.TrimSpace(name) == "" || projektoveID <= 0 {
		return fmt.Errorf("name and valid projektove_id are required: %w", ErrInvalidArgument)
	}

	org, err := c.Repository.GetProjektoveOrganizationByUUID(ctx, orgUUID)
	if err != nil {
		return fmt.Errorf("when getting organization: %w", err)
	}

	if err := c.Repository.StoreOrganizationUser(ctx, org.ID, name, projektoveID); err != nil {
		return fmt.Errorf("when storing organization user: %w", err)
	}

	return nil
}

func (c Controller) RemoveOrganizationUser(ctx context.Context, orgUUID string, userUUID string) error {
	org, err := c.Repository.GetProjektoveOrganizationByUUID(ctx, orgUUID)
	if err != nil {
		return fmt.Errorf("when getting organization: %w", err)
	}

	if err := c.Repository.DeleteOrganizationUser(ctx, org.ID, userUUID); err != nil {
		return fmt.Errorf("when deleting organization user: %w", err)
	}

	return nil
}

func (c Controller) ListAdminModels(ctx context.Context) ([]Provider, []Model, error) {
	providers, err := c.Repository.ListProviders(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("when listing providers: %w", err)
	}

	models, err := c.Repository.ListModels(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("when listing models: %w", err)
	}

	return providers, models, nil
}

func (c Controller) CreateProvider(ctx context.Context, provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider is required: %w", ErrInvalidArgument)
	}

	providers, err := c.Repository.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("when listing providers: %w", err)
	}
	for _, p := range providers {
		if strings.EqualFold(p.Provider, provider) {
			return fmt.Errorf("%w: %s", ErrProviderExists, provider)
		}
	}

	if _, err := c.Repository.GetOrCreateProvider(ctx, provider); err != nil {
		return fmt.Errorf("when creating provider: %w", err)
	}

	return nil
}

func (c Controller) CreateModel(ctx context.Context, provider, model string) error {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("provider and model are required: %w", ErrInvalidArgument)
	}

	providerID, err := c.Repository.GetOrCreateProvider(ctx, provider)
	if err != nil {
		return fmt.Errorf("when getting provider: %w", err)
	}

	if _, _, err := c.Repository.GetOrCreateModel(ctx, providerID, model); err != nil {
		return fmt.Errorf("when creating model: %w", err)
	}

	return nil
}
