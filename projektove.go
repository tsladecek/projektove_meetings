package projektovemeeting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type ProjektoveAPI struct {
	client  *Client
	baseURL *url.URL
	token   string
}

type projektoveCreateBody struct {
	Issue ProjektoveIssueCreate `json:"issue"`
}

func NewProjektoveAPI(baseURL, token string, client *Client) (Projektove, error) {
	burl, err := url.Parse(baseURL)
	if err != nil {
		return ProjektoveAPI{}, fmt.Errorf("when parsing projektove url %q: %w", baseURL, err)
	}

	if client == nil {
		return ProjektoveAPI{}, fmt.Errorf("no http client provided")
	}
	return ProjektoveAPI{
		baseURL: burl,
		token:   token,
		client:  client,
	}, nil
}

func (p ProjektoveAPI) CreateIssue(ctx context.Context, obj ProjektoveIssueCreate) error {
	u := p.baseURL.JoinPath("issues.json").String()

	body := projektoveCreateBody{
		Issue: obj,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal update body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("when creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Authorization", p.token)

	if _, err := p.client.Do(req, nil); err != nil {
		return fmt.Errorf("when creating issue :%w", err)
	}

	return nil
}

type ResponseProjects struct {
	Projects []ProjektoveProject `json:"projects"`
}

func (p ProjektoveAPI) GetProjects(ctx context.Context) ([]ProjektoveProject, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL.JoinPath("projects.json").String(), nil)
	if err != nil {
		return nil, fmt.Errorf("when creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Authorization", p.token)

	projects := ResponseProjects{}

	if _, err := p.client.Do(req, &projects); err != nil {
		return nil, fmt.Errorf("when fetching projects: %w", err)
	}

	return projects.Projects, nil
}
