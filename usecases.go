package projektovemeeting

import (
	"context"
	"encoding/json"
	"errors"
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

func (c Controller) Infer(ctx context.Context, user User, modelProvider string, modelName string, contextID int, meeting string, users []ProjektoveUser) ([]Issue, int, error) {
	projects, err := c.Projektove.GetProjects(ctx, user)
	if err != nil {
		return nil, 0, fmt.Errorf("when listing projects: %w", err)
	}

	model, found := user.GetModel(LLMProvider(modelProvider), modelName)
	if !found {
		return nil, 0, ErrModelNotFound
	}

	llm, err := c.NewLLMProvider(model.Provider, model.Model, model.Token)
	if err != nil {
		return nil, 0, fmt.Errorf("when setting up llm: %w", err)
	}

	projectsMarshalled, err := json.Marshal(projects)
	if err != nil {
		return nil, 0, fmt.Errorf("when encoding projects")
	}
	usersMarshalled, err := json.Marshal(users)
	if err != nil {
		return nil, 0, fmt.Errorf("when encoding users")
	}

	generalContext, err := c.Repository.GetContext(ctx, user, contextID)
	if err != nil {
		return nil, 0, fmt.Errorf("when fetching context %d", contextID)
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
	`, usersMarshalled, projectsMarshalled, generalContext.Context, meeting)

	issues := []Issue{}
	promptID := 0

	err = c.TxProvider.Transact(func(repo Repository) error {
		slog.Debug("Inferring...")
		inference, err := llm.Infer(ctx, prompt)
		if err != nil {
			resultErr := err
			_, err := c.Repository.StorePrompt(ctx, user, PromptCreate{Prompt: prompt, Result: inference, Error: err, ContextID: contextID})
			if err != nil {
				resultErr = errors.Join(err, fmt.Errorf("when storing prompt: %w", err))
			}
			return fmt.Errorf("when prompting the llm: %w", resultErr)
		}

		slog.Debug("Inference done")

		promptID, err = c.Repository.StorePrompt(ctx, user, PromptCreate{Prompt: prompt, Result: inference, Error: nil, ContextID: contextID})
		if err != nil {
			slog.Error("Error occured while storing prompt", "err", err.Error())
		}

		objs := []IssueCreate{}
		if err := json.Unmarshal([]byte(inference), &objs); err != nil {
			return fmt.Errorf("when unmarshalling response: %w", err)
		}

		for _, iss := range objs {
			iss.Parent = IssueParentPrompt
			iss.ParentID = promptID
			id, err := repo.StoreIssue(ctx, user, iss)
			if err != nil {
				return fmt.Errorf("when storing issue: %w", err)
			}

			issues = append(issues, iss.ToDomain(id))
		}
		return nil
	})

	if err != nil {
		return nil, 0, fmt.Errorf("when running inference and storing results: %w", err)
	}

	return issues, promptID, nil
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
	_, promptID, err := c.Infer(ctx, user, modelProvider, modelName, contextID, meeting, c.Users)
	if err != nil {
		return 0, err
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
