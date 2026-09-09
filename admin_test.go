package projektovemeeting

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createAdminUserWithPassword(t *testing.T, repo Repository, email, password string) User {
	t.Helper()
	hash, err := HashPassword(password)
	require.NoError(t, err)
	_, err = repo.StoreUser(t.Context(), UserCreate{Email: email, PasswordHash: &hash, IsAdmin: true})
	require.NoError(t, err)
	user, err := repo.GetUser(t.Context(), email)
	require.NoError(t, err)
	require.True(t, user.IsAdmin)
	return user
}

func newAdminTestServer(t *testing.T, repo Repository) (http.Handler, *AuthComposite, User, User) {
	t.Helper()
	auth := newTestAuth(t, repo)
	user := createUserWithPassword(t, repo, "user@example.com", "secret")
	admin := createAdminUserWithPassword(t, repo, "admin@example.com", "secret")

	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)
	logout := NewEndpoint(http.MethodGet, "", "/logout")
	handler := NewHandler(auth, baseURL.String(), Controller{Repository: repo}, logout)

	return handler, auth.(*AuthComposite), user, admin
}

func adminRequest(handler http.Handler, auth *AuthComposite, user User, method, path, body string) *httptest.ResponseRecorder {
	token, err := auth.generateSessionToken(user.ID)
	if err != nil {
		panic(err)
	}

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminOrganizationsPage_NonAdminForbidden(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, user, http.MethodGet, "/admin/organizations", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "Forbidden")
}

func TestAdminOrganizationsPage_AdminOK(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Organizations")
}

func TestAdminCreateOrganization_NonAdminForbidden(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations", "")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = adminRequest(handler, auth, user, http.MethodPost, "/api/admin/organizations", url.Values{
		"name":        {"acme"},
		"api_url":     {"https://api.example.com"},
		"browser_url": {"https://app.example.com"},
	}.Encode())
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminCreateOrganization_AdminCreatesOrg(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/organizations", url.Values{
		"name":        {"acme"},
		"api_url":     {"https://api.example.com"},
		"browser_url": {"https://app.example.com"},
	}.Encode())
	assert.Equal(t, http.StatusSeeOther, rec.Code)

	orgs, err := repo.ListProjektoveOrganizations(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "acme", orgs[0].Name)

	rec = adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+strconv.Itoa(orgs[0].ID), "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "acme")
}

func TestAdminCreateOrganization_ValidationErrorRerendersForm(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/organizations", url.Values{
		"name":        {""},
		"api_url":     {"https://api.example.com"},
		"browser_url": {"https://app.example.com"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "could not be created")

	orgs, err := repo.ListProjektoveOrganizations(t.Context())
	require.NoError(t, err)
	assert.Empty(t, orgs)
}

func TestAdminModelsPage_AdminOK(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodGet, "/admin/models", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Models")
	assert.Contains(t, rec.Body.String(), "Provider")
	assert.Contains(t, rec.Body.String(), "admin-models-list")
}

func TestAdminOrganizationDetail_AdminOK(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.StoreOrganizationUser(t.Context(), orgID, "jane", 42))

	rec := adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+strconv.Itoa(orgID), "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Organization: acme")
	assert.Contains(t, rec.Body.String(), "jane")

	// non-admin cannot view the detail page
	rec = adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+strconv.Itoa(orgID), "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAdminUpdateOrganization_AdminUpdates(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/organizations/"+strconv.Itoa(orgID), url.Values{
		"name":        {"acme2"},
		"api_url":     {"https://api2.example.com"},
		"browser_url": {"https://app2.example.com"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "acme2")

	got, err := repo.GetProjektoveOrganization(t.Context(), orgID)
	require.NoError(t, err)
	assert.Equal(t, "acme2", got.Name)
	assert.Equal(t, "https://api2.example.com", got.APIURL)
}

func TestAdminAddOrgUser_AddsUser(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/organizations/"+strconv.Itoa(orgID)+"/users", url.Values{
		"name":         {"jane"},
		"projektove_id": {"42"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "jane")

	users, err := repo.ListOrganizationUsers(t.Context(), orgID)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "jane", users[0].Name)
	assert.Equal(t, 42, users[0].ProjektoveID)
}

func TestAdminRemoveOrgUser_RemovesUser(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.StoreOrganizationUser(t.Context(), orgID, "jane", 42))

	users, err := repo.ListOrganizationUsers(t.Context(), orgID)
	require.NoError(t, err)
	require.Len(t, users, 1)

	rec := adminRequest(handler, auth, admin, http.MethodDelete, "/api/admin/organizations/"+strconv.Itoa(orgID)+"/users/"+strconv.Itoa(users[0].ID), "")
	assert.Equal(t, http.StatusOK, rec.Code)

	users, err = repo.ListOrganizationUsers(t.Context(), orgID)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestAdminCreateModel_AdminCreates(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/models", url.Values{
		"provider": {"openai"},
		"model":    {"gpt-4o"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "gpt-4o")

	providers, err := repo.ListProviders(t.Context())
	require.NoError(t, err)
	require.Len(t, providers, 1)
	assert.Equal(t, "openai", providers[0].Provider)

	models, err := repo.ListModels(t.Context())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "gpt-4o", models[0].Model)
}

func TestAddLLMModel_SelectValueSplitsProviderModel(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, user, http.MethodPost, "/api/models", url.Values{
		"model": {"openai/gpt-4o"},
		"token": {"secret-token"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="model_provider"`)
	assert.Contains(t, rec.Body.String(), `value="openai"`)
	assert.Contains(t, rec.Body.String(), `value="gpt-4o"`)
	assert.Contains(t, rec.Body.String(), `value="secret-token"`)
}

func TestAddLLMModel_InvalidSelectValue(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, user, http.MethodPost, "/api/models", url.Values{
		"model": {"not-a-valid-value"},
		"token": {"secret-token"},
	}.Encode())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUserPage_RendersModelSelect(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)
	providerID, err := repo.GetOrCreateProvider(t.Context(), "openai")
	require.NoError(t, err)
	_, _, err = repo.GetOrCreateModel(t.Context(), providerID, "gpt-4o")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, user, http.MethodGet, "/user", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `value="openai/gpt-4o"`)
	assert.Contains(t, rec.Body.String(), `name="model"`)
}