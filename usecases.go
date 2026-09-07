package projektovemeeting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	  "description": "id of the person to assign the issue to. You can infer this from the users array above. Leave it empty if not sure"
	},
  },
  "required": ["subject", "description", "assigned_to_id"],
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
		profile.Contexts = append(profile.Contexts, ContextView{ID: c.ID, Name: c.Name, Context: c.Context})
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
	id, err := c.Repository.StoreContext(ctx, user, v)
	if err != nil {
		return ContextView{}, fmt.Errorf("when storing context: %w", err)
	}

	return ContextView{ID: id, Name: v.Name, Context: v.Context}, nil
}

func (c Controller) DeleteContext(ctx context.Context, user User, id int) error {
	if err := c.Repository.DeleteContext(ctx, user, id); err != nil {
		return err
	}

	return nil
}

func (c Controller) CreatePrompt(ctx context.Context, user User, modelProvider, modelName string, contextID int, meeting string) (int, error) {
	if _, found := user.GetModel(LLMProvider(modelProvider), modelName); !found {
		return 0, ErrModelNotFound
	}

	promptID := 0
	if err := c.TxProvider.Transact(func(repo Repository) error {
		id, err := repo.StorePrompt(ctx, user, PromptCreate{
			ContextID:   contextID,
			Status:      PromptStatusCreated,
			Provider:    modelProvider,
			Model:       modelName,
			FileContent: meeting,
		})
		if err != nil {
			return fmt.Errorf("when storing prompt: %w", err)
		}
		promptID = id

		if _, err := repo.EnqueueTask(ctx, TaskCreate{Type: TaskTypeInference, Payload: InferenceJob{UserID: user.ID, PromptID: id}}); err != nil {
			return fmt.Errorf("when enqueuing inference task: %w", err)
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("when creating prompt: %w", err)
	}

	return promptID, nil
}

func (c Controller) ListContexts(ctx context.Context, user User) ([]ContextView, error) {
	contexts, err := c.Repository.ListContexts(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("when listing contexts: %w", err)
	}

	views := make([]ContextView, 0, len(contexts))
	for _, c := range contexts {
		views = append(views, ContextView{ID: c.ID, Name: c.Name, Context: c.Context})
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
			ID:              p.ID,
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

func (c Controller) GetPrompt(ctx context.Context, user User, id int) (PromptView, error) {
	prompt, err := c.Repository.GetPrompt(ctx, user, id)
	if err != nil {
		return PromptView{}, err
	}

	issues, err := c.Repository.ListIssues(ctx, user, IssueParentPrompt, id)
	if err != nil {
		return PromptView{}, fmt.Errorf("when listing issues: %w", err)
	}

	view := PromptView{
		ID:          prompt.ID,
		Prompt:      prompt.Prompt,
		Result:      prompt.Result,
		Error:       prompt.Error,
		ContextName: prompt.Context.Name,
		Status:      prompt.Status,
		Issues:      make([]IssueView, 0, len(issues)),
	}

	for _, iss := range issues {
		view.Issues = append(view.Issues, IssueView{
			ID:           iss.ID,
			Subject:      iss.Subject,
			Description:  iss.Description,
			ProjectID:    iss.ProjectID,
			StartDate:    iss.StartDate,
			DueDate:      iss.DueDate,
			AssignedToID: iss.AssignedToID,
			ProjektoveID: iss.ProjektoveID,
			Status:       iss.Status,
			Editable:     !isSubmitted(iss.Status),
		})
	}

	return view, nil
}

func (c Controller) UpdateIssue(ctx context.Context, user User, promptID, issueID int, v IssueUpdateView) error {
	iss, err := c.Repository.GetIssue(ctx, user, IssueParentPrompt, promptID, issueID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
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

	if err := c.Repository.UpdateIssue(ctx, user, IssueParentPrompt, promptID, issueID, obj); err != nil {
		return fmt.Errorf("when updating issue: %w", err)
	}

	return nil
}

func (c Controller) SubmitIssue(ctx context.Context, user User, promptID, issueID int) error {
	iss, err := c.Repository.GetIssue(ctx, user, IssueParentPrompt, promptID, issueID)
	if err != nil {
		return err
	}

	if isSubmitted(iss.Status) {
		return ErrIssueSubmitted
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
		if updateErr := c.Repository.UpdateIssue(ctx, user, IssueParentPrompt, promptID, issueID, IssueUpdate{
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

	if err := c.Repository.UpdateIssue(ctx, user, IssueParentPrompt, promptID, issueID, IssueUpdate{
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
