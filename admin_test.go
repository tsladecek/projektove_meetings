package projektovemeeting

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	handler := NewHandler(auth, baseURL, Controller{Repository: repo}, logout)

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
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("HX-Location"))

	orgs, err := repo.ListProjektoveOrganizations(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "acme", orgs[0].Name)

	rec = adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+orgs[0].UUID, "")
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
	assert.Contains(t, rec.Body.String(), "admin-models-list")
	assert.Contains(t, rec.Body.String(), "add-provider-form")
	assert.Contains(t, rec.Body.String(), "add-model-form")
	assert.Contains(t, rec.Body.String(), `name="provider"`)
	assert.Contains(t, rec.Body.String(), "No providers yet")
}

func TestAdminOrganizationDetail_AdminOK(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, orgUUID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.StoreOrganizationUser(t.Context(), orgID, "jane", 42))

	rec := adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+orgUUID, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Organization: acme")
	assert.Contains(t, rec.Body.String(), "jane")

	// non-admin cannot view the detail page
	rec = adminRequest(handler, auth, admin, http.MethodGet, "/admin/organizations/"+orgUUID, "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAdminUpdateOrganization_AdminUpdates(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	_, orgUUID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, admin, http.MethodPut, "/api/admin/organizations/"+orgUUID, url.Values{
		"name":        {"acme2"},
		"api_url":     {"https://api2.example.com"},
		"browser_url": {"https://app2.example.com"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "https://api2.example.com")

	got, err := repo.GetProjektoveOrganizationByUUID(t.Context(), orgUUID)
	require.NoError(t, err)
	assert.Equal(t, "https://api2.example.com", got.APIURL)
}

func TestAdminAddOrgUser_AddsUser(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	orgID, orgUUID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/organizations/"+orgUUID+"/users", url.Values{
		"name":          {"jane"},
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
	orgID, orgUUID, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.StoreOrganizationUser(t.Context(), orgID, "jane", 42))

	users, err := repo.ListOrganizationUsers(t.Context(), orgID)
	require.NoError(t, err)
	require.Len(t, users, 1)

	rec := adminRequest(handler, auth, admin, http.MethodDelete, "/api/admin/organizations/"+orgUUID+"/users/"+users[0].UUID, "")
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

func TestAdminCreateProvider_AdminCreates(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/providers", url.Values{
		"provider": {"openai"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Success!", rec.Header().Get("X-Toast"))
	assert.Contains(t, rec.Body.String(), "admin-models-content")
	assert.Contains(t, rec.Body.String(), `value="openai"`)

	providers, err := repo.ListProviders(t.Context())
	require.NoError(t, err)
	require.Len(t, providers, 1)
	assert.Equal(t, "openai", providers[0].Provider)
}

func TestAdminCreateProvider_Invalid(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/providers", url.Values{
		"provider": {""},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Provider is required", rec.Header().Get("X-Error"))
}

func TestAdminCreateProvider_Duplicate(t *testing.T) {
	repo := newRepository(t)
	handler, auth, _, admin := newAdminTestServer(t, repo)
	_, err := repo.GetOrCreateProvider(t.Context(), "openai")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, admin, http.MethodPost, "/api/admin/providers", url.Values{
		"provider": {"openai"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Provider already exists", rec.Header().Get("X-Error"))

	providers, err := repo.ListProviders(t.Context())
	require.NoError(t, err)
	require.Len(t, providers, 1)
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
	_, _, err = repo.GetOrCreateModel(t.Context(), providerID, "gpt-4o-mini")
	require.NoError(t, err)
	storeUserModel(t, repo, user, "openai", "gpt-4o", "g-token")

	rec := adminRequest(handler, auth, user, http.MethodGet, "/user", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="model"`)
	assert.Contains(t, rec.Body.String(), `value="openai/gpt-4o-mini"`)
	assert.NotContains(t, rec.Body.String(), `value="openai/gpt-4o"`)
}

func TestAddOrganization_Valid(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)
	_, _, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, user, http.MethodPost, "/api/organizations", url.Values{
		"organization": {"acme"},
		"token":        {"secret-token"},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="org_name"`)
	assert.Contains(t, rec.Body.String(), `value="acme"`)
	assert.Contains(t, rec.Body.String(), `value="secret-token"`)
}

func TestAddOrganization_Unknown(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)

	rec := adminRequest(handler, auth, user, http.MethodPost, "/api/organizations", url.Values{
		"organization": {"missing"},
		"token":        {"secret-token"},
	}.Encode())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateUser_SavesNewOrganization(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)
	_, _, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, user, http.MethodPut, "/api/user", url.Values{
		"org_name":  {"acme"},
		"org_token": {"org-token"},
		"org_uuid":  {""},
	}.Encode())
	assert.Equal(t, http.StatusOK, rec.Code)

	orgs, err := repo.ListUserProjektoveOrganizations(t.Context(), user.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "acme", orgs[0].OrgName)
	assert.Equal(t, "org-token", orgs[0].Token)
}

func TestUserPage_RendersOrgSelect(t *testing.T) {
	repo := newRepository(t)
	handler, auth, user, _ := newAdminTestServer(t, repo)
	_, _, err := repo.StoreProjektoveOrganization(t.Context(), "acme", "https://api.example.com", "https://app.example.com")
	require.NoError(t, err)

	rec := adminRequest(handler, auth, user, http.MethodGet, "/user", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="organization"`)
	assert.Contains(t, rec.Body.String(), `value="acme"`)
	assert.Contains(t, rec.Body.String(), `id="add-organization-form"`)
}
