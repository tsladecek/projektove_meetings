package projektovemeeting

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingTransport records how many requests it handles and returns a canned response.
type countingTransport struct {
	mu       sync.Mutex
	requests int
	status   int
	body     string
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests++
	t.mu.Unlock()
	return &http.Response{
		StatusCode: t.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    req,
	}, nil
}

func (t *countingTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requests
}

func newProjektoveAPI(tr *countingTransport, db Repository) Projektove {
	if tr.status == 0 {
		tr.status = http.StatusOK
	}
	api, err := NewProjektoveAPI("http://example.test", NewClient(WithHTTPClient(&http.Client{Transport: tr})), db)
	if err != nil {
		panic(err)
	}
	return api
}

func TestProjektoveGetProjects_TokenMissing(t *testing.T) {
	tr := &countingTransport{}
	api := newProjektoveAPI(tr, nil)

	projects, err := api.GetProjects(t.Context(), User{Email: "u@example.com"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))
	assert.Nil(t, projects)
	assert.Zero(t, tr.count())
}

func TestProjektoveGetProjects_WhitespaceTokenMissing(t *testing.T) {
	tr := &countingTransport{}
	api := newProjektoveAPI(tr, nil)

	_, err := api.GetProjects(t.Context(), User{Email: "u@example.com", ProjektoveToken: "   "})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))
	assert.Zero(t, tr.count())
}

func TestProjektoveCreateIssue_TokenMissing(t *testing.T) {
	tr := &countingTransport{}
	api := newProjektoveAPI(tr, nil)

	_, err := api.CreateIssue(t.Context(), User{Email: "u@example.com"}, ProjektoveIssueCreate{Subject: "s"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))
	assert.Zero(t, tr.count())
}

func TestProjektoveGetProjects_TokenMissing_IgnoresCache(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "u@example.com")

	require.NoError(t, repo.UpdateProjectsCache(t.Context(), user, []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}))

	tr := &countingTransport{}
	api := newProjektoveAPI(tr, repo)

	_, err := api.GetProjects(t.Context(), User{Email: "u@example.com"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjektoveTokenNotConfigured))
	assert.Zero(t, tr.count())
}

func TestProjektoveGetProjects_WithToken(t *testing.T) {
	repo := newRepository(t)
	user := storeUser(t, repo, "u@example.com")
	require.NoError(t, repo.UpdateUser(t.Context(), user, UserUpdate{ProjektoveToken: "secret"}))

	tr := &countingTransport{
		status: http.StatusOK,
		body:   `{"projects":[{"id":1,"name":"p1","description":"d1"},{"id":2,"name":"p2","description":"d2"}]}`,
	}
	api := newProjektoveAPI(tr, repo)

	projects, err := api.GetProjects(t.Context(), User{Email: "u@example.com", ProjektoveToken: "secret"})
	require.NoError(t, err)
	require.Len(t, projects, 2)
	assert.Equal(t, 1, projects[0].ID)
	assert.Equal(t, "p1", projects[0].Name)
	assert.Equal(t, 1, tr.count())
}

func TestProjektoveCreateIssue_WithToken(t *testing.T) {
	tr := &countingTransport{
		status: http.StatusCreated,
		body:   `{"issue":{"id":7,"subject":"s"}}`,
	}
	api := newProjektoveAPI(tr, nil)

	issue, err := api.CreateIssue(t.Context(), User{ProjektoveToken: "secret"}, ProjektoveIssueCreate{Subject: "s", ProjectID: 1})
	require.NoError(t, err)
	assert.Equal(t, 7, issue.ID)
	assert.Equal(t, 1, tr.count())
}
