package projektovemeeting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

type Controller struct {
	DB         DB
	Projektove Projektove
	LLM        LLM
}

func (c Controller) Infer(ctx context.Context, contextID int, meeting string, users []ProjektoveUser) ([]ProjektoveIssueCreate, error) {
	projects, err := c.Projektove.GetProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("when listing projects: %w", err)
	}

	projectsMarshalled, err := json.Marshal(projects)
	if err != nil {
		return nil, fmt.Errorf("when encoding projects")
	}
	usersMarshalled, err := json.Marshal(users)
	if err != nil {
		return nil, fmt.Errorf("when encoding users")
	}

	generalContext, err := c.DB.GetContext(ctx, contextID)
	if err != nil {
		return nil, fmt.Errorf("when fetching context %d", contextID)
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
	`, usersMarshalled, projectsMarshalled, generalContext, meeting)

	var response string

	defer func() {
		c.DB.StorePrompt(ctx, PromptCreate{Prompt: prompt, Result: response, Error: err, ContextID: contextID})
	}()

	slog.Debug("Inferring...")
	response, err = c.LLM.Infer(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("when prompting the llm: %w", err)
	}

	slog.Debug("Inference done")

	issues := []ProjektoveIssueCreate{}
	if err := json.Unmarshal([]byte(response), &issues); err != nil {
		return nil, fmt.Errorf("when unmarshalling response: %w", err)
	}

	return issues, nil
}
