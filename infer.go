package projektovemeeting

import (
	"context"
	"encoding/json"
	"fmt"
)

func Infer(ctx context.Context, projektove Projektove, llm LLM, context, meeting string, users []ProjektoveUser) error {
	projects, err := projektove.GetProjects(ctx)
	if err != nil {
		return fmt.Errorf("when listing projects: %w", err)
	}

	projectsMarshalled, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("when encoding projects")
	}
	usersMarshalled, err := json.Marshal(users)
	if err != nil {
		return fmt.Errorf("when encoding users")
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
    "issue": {
      "type": "object",
      "required": ["subject", "description"],
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
          "type": "string",
          "description": "id of the person to assign the issue to. You can infer this from the users array above. Leave it empty if not sure"
        },
      },
      "additionalProperties": true
    }
  },
  "required": [
    "issue"
  ],
  "additionalProperties": false
}

	Meeting notes:
	%s
	`, usersMarshalled, projectsMarshalled, context, meeting)

	response, err := llm.Infer(ctx, prompt)
	if err != nil {
		return fmt.Errorf("when prompting the llm: %w", err)
	}

	fmt.Printf("Response: \n%+v", response)
	return nil
}
