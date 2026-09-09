package projektovemeeting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ProjektoveAPI struct {
	client *Client
}

type projektoveCreateBody struct {
	Issue ProjektoveIssueCreate `json:"issue"`
}

func NewProjektoveAPI(client *Client) (Projektove, error) {
	if client == nil {
		return ProjektoveAPI{}, fmt.Errorf("no http client provided")
	}
	return ProjektoveAPI{
		client: client,
	}, nil
}

type ResponseCreateIssue struct {
	Issue ProjektoveIssue `json:"issue"`
}

func (p ProjektoveAPI) CreateIssue(ctx context.Context, token, baseURL string, obj ProjektoveIssueCreate) (ProjektoveIssue, error) {
	if strings.TrimSpace(token) == "" {
		return ProjektoveIssue{}, ErrProjektoveTokenNotConfigured
	}

	u := strings.TrimSuffix(baseURL, "/") + "/issues.json"

	body := projektoveCreateBody{
		Issue: obj,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ProjektoveIssue{}, fmt.Errorf("marshal update body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return ProjektoveIssue{}, fmt.Errorf("when creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Authorization", token)

	response := ResponseCreateIssue{}

	if _, err := p.client.Do(req, &response); err != nil {
		return ProjektoveIssue{}, fmt.Errorf("when creating issue :%w", err)
	}

	return response.Issue, nil
}

type ResponseProjects struct {
	Projects []ProjektoveProject `json:"projects"`
}

func (p ProjektoveAPI) GetProjects(ctx context.Context, token, baseURL string) ([]ProjektoveProject, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrProjektoveTokenNotConfigured
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/projects.json", nil)
	if err != nil {
		return nil, fmt.Errorf("when creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Authorization", token)

	projects := ResponseProjects{}

	if _, err := p.client.Do(req, &projects); err != nil {
		return nil, fmt.Errorf("when fetching projects: %w", err)
	}

	return projects.Projects, nil
}
