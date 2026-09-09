package projektovemeeting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

type Controller struct {
	TxProvider     *TxProvider
	Repository     Repository
	Projektove     Projektove
	NewLLMProvider func(provider LLMProvider, model string, token string) (LLM, error)
	Users          ProjektoveUsers
}

func (c Controller) RunInference(ctx context.Context, job InferenceJob) error {
	user, err := c.Repository.GetUserByID(ctx, job.UserID)
	if err != nil {
		return fmt.Errorf("when loading user %d: %w", job.UserID, err)
	}

	prompt, err := c.Repository.GetPrompt(ctx, user, job.PromptID)
	if err != nil {
		return fmt.Errorf("when loading prompt %d: %w", job.PromptID, err)
	}
	if !prompt.Status.IsPending() {
		return nil
	}

	projects, err := c.Projektove.GetProjects(ctx, user)
	if err != nil {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{}, fmt.Errorf("when listing projects: %w", err))
		return err
	}

	model, found := user.GetModel(LLMProvider(prompt.Provider), prompt.Model)
	if !found {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{}, ErrModelNotFound)
		return ErrModelNotFound
	}

	llm, err := c.NewLLMProvider(model.Provider, model.Model, model.Token)
	if err != nil {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{}, fmt.Errorf("when setting up llm: %w", err))
		return err
	}

	promptText, err := buildInferencePrompt(projects, c.Users, prompt.Context.Context, prompt.FileContent)
	if err != nil {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{}, err)
		return err
	}

	if err := c.Repository.SetPromptProcessing(ctx, user, job.PromptID, promptText); err != nil {
		return fmt.Errorf("when marking prompt as processing: %w", err)
	}

	slog.Debug("Inferring...", "prompt_id", job.PromptID)
	inference, err := llm.Infer(ctx, promptText)
	slog.Debug("Inference done", "prompt_id", job.PromptID)
	if err != nil {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{Prompt: promptText, Result: inference}, err)
		return err
	}

	objs := []IssueCreate{}
	if err := json.Unmarshal([]byte(inference), &objs); err != nil {
		c.failPrompt(ctx, user, job.PromptID, PromptComplete{Prompt: promptText, Result: inference}, fmt.Errorf("when unmarshalling response: %w", err))
		return err
	}

	if err := c.TxProvider.Transact(func(repo Repository) error {
		for _, iss := range objs {
			iss.Parent = IssueParentPrompt
			iss.ParentID = job.PromptID
			if _, err := repo.StoreIssue(ctx, user, iss); err != nil {
				return fmt.Errorf("when storing issue: %w", err)
			}
		}
		if err := repo.CompletePrompt(ctx, user, job.PromptID, PromptComplete{Prompt: promptText, Result: inference, Status: PromptStatusDone}); err != nil {
			return fmt.Errorf("when marking prompt as done: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("when storing inference results: %w", err)
	}

	return nil
}

func (c Controller) failPrompt(ctx context.Context, user User, promptID int, partial PromptComplete, cause error) {
	slog.Error("Inference failed", "prompt_id", promptID, "err", cause.Error())
	if err := c.Repository.CompletePrompt(ctx, user, promptID, PromptComplete{
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

func (c Controller) GetUserProfile(ctx context.Context, user User) (UserProfileView, error) {
	profile := UserProfileView{
		Email:           user.Email,
		ProjektoveToken: user.ProjektoveToken,
	}

	for _, m := range user.LLMModels {
		profile.Models = append(profile.Models, LLMModelView{Provider: m.Provider, Model: m.Model, Token: m.Token})
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
	obj := UserUpdate{
		ProjektoveToken: v.ProjektoveToken,
		UpdateModels:    v.Models != nil,
		Models:          []LLMModel{},
	}
	for _, m := range v.Models {
		obj.Models = append(obj.Models, LLMModel{Provider: m.Provider, Model: m.Model, Token: m.Token})
	}

	if err := c.Repository.UpdateUser(ctx, user, obj); err != nil {
		return fmt.Errorf("when updating user: %w", err)
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

func (c Controller) CreatePrompt(ctx context.Context, user User, modelProvider, modelName string, contextID int, meeting string) (string, error) {
	if _, err := c.Projektove.GetProjects(ctx, user); err != nil {
		return "", fmt.Errorf("when listing projects: %w", err)
	}

	if _, found := user.GetModel(LLMProvider(modelProvider), modelName); !found {
		return "", ErrModelNotFound
	}

	promptUUID := ""
	if err := c.TxProvider.Transact(func(repo Repository) error {
		id, uuid, err := repo.StorePrompt(ctx, user, PromptCreate{
			ContextID:   contextID,
			Status:      PromptStatusCreated,
			Provider:    modelProvider,
			Model:       modelName,
			FileContent: meeting,
		})
		if err != nil {
			return fmt.Errorf("when storing prompt: %w", err)
		}
		promptUUID = uuid

		if _, err := repo.EnqueueTask(ctx, TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: user.ID, PromptID: id}}); err != nil {
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

func (c Controller) ListProjects(ctx context.Context, user User) ([]ProjectOptionView, error) {
	projects, err := c.Projektove.GetProjects(ctx, user)
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
	prompt, err := c.Repository.GetPromptByUUID(ctx, user, uuid)
	if err != nil {
		return PromptView{}, err
	}
	return c.buildPromptView(ctx, user, prompt)
}

// GetIssueViewByUUID resolves an issue by its uuid and returns a single issue
// view, regardless of its parent (prompt or batch). Used to re-render an
// issue card after update/submit.
func (c Controller) GetIssueViewByUUID(ctx context.Context, user User, issueUUID string) (IssueView, error) {
	iss, err := c.Repository.GetIssueByUUID(ctx, user, issueUUID)
	if err != nil {
		return IssueView{}, err
	}
	return toIssueView(iss), nil
}

func (c Controller) buildPromptView(ctx context.Context, user User, prompt Prompt) (PromptView, error) {
	issues, err := c.Repository.ListIssues(ctx, user, IssueParentPrompt, prompt.ID)
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
	}

	return view, nil
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

func (c Controller) CreateBatch(ctx context.Context, user User, rawCSV string) (string, error) {
	projects, err := c.Projektove.GetProjects(ctx, user)
	if err != nil {
		return "", fmt.Errorf("when listing projects: %w", err)
	}

	objs, err := parseBatchCSV(rawCSV, projects, c.Users)
	if err != nil {
		return "", err
	}

	batchUUID := ""
	if err := c.TxProvider.Transact(func(repo Repository) error {
		id, uuid, err := repo.StoreBatch(ctx, user, rawCSV)
		if err != nil {
			return fmt.Errorf("when storing batch: %w", err)
		}
		batchUUID = uuid

		for _, iss := range objs {
			iss.Parent = IssueParentBatch
			iss.ParentID = id
			if _, err := repo.StoreIssue(ctx, user, iss); err != nil {
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
	batch, err := c.Repository.GetBatchByUUID(ctx, user, uuid)
	if err != nil {
		return BatchView{}, err
	}
	return c.buildBatchView(ctx, user, batch)
}

func (c Controller) buildBatchView(ctx context.Context, user User, batch Batch) (BatchView, error) {
	issues, err := c.Repository.ListIssues(ctx, user, IssueParentBatch, batch.ID)
	if err != nil {
		return BatchView{}, fmt.Errorf("when listing issues: %w", err)
	}

	return BatchView{
		ID:          batch.UUID,
		FileContent: batch.FileContent,
		CreatedAt:   batch.CreatedAt,
		Issues:      toIssueViews(issues),
	}, nil
}

func (c Controller) UpdateIssue(ctx context.Context, user User, issueUUID string, v IssueUpdateView) error {
	iss, err := c.Repository.GetIssueByUUID(ctx, user, issueUUID)
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

	if err := c.Repository.UpdateIssue(ctx, user, iss.Parent, iss.ParentID, iss.ID, obj); err != nil {
		return fmt.Errorf("when updating issue: %w", err)
	}

	return nil
}

func (c Controller) SubmitIssue(ctx context.Context, user User, issueUUID string) error {
	iss, err := c.Repository.GetIssueByUUID(ctx, user, issueUUID)
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

	created, err := c.Projektove.CreateIssue(ctx, user, obj)
	if err != nil {
		failStatus := IssueStatusSubmitFailed
		if updateErr := c.Repository.UpdateIssue(ctx, user, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
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

	if err := c.Repository.UpdateIssue(ctx, user, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
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
	iss, err := c.Repository.GetIssueByUUID(ctx, user, issueUUID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
	}
	if iss.Status == IssueStatusIgnored {
		return nil
	}

	if err := c.Repository.UpdateIssue(ctx, user, iss.Parent, iss.ParentID, iss.ID, IssueUpdate{
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
