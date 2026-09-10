package projektovemeeting

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAuth(t *testing.T, repo Repository) Auth {
	t.Helper()
	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)

	auth, err := NewAuth(repo, nil, "test-secret-key",
		NewEndpoint(http.MethodGet, baseURL.Path, "/login"),
		NewEndpoint(http.MethodGet, baseURL.Path, "/logout"),
		baseURL)
	require.NoError(t, err)
	return auth
}

func createUserWithPassword(t *testing.T, repo Repository, email, password string) User {
	t.Helper()
	hash, err := HashPassword(password)
	require.NoError(t, err)
	_, err = repo.StoreUser(t.Context(), UserCreate{Email: email, PasswordHash: &hash})
	require.NoError(t, err)
	user, err := repo.GetUser(t.Context(), email)
	require.NoError(t, err)
	return user
}

func TestSelfLogin_POSTSuccess(t *testing.T) {
	repo := newRepository(t)
	createUserWithPassword(t, repo, "user@example.com", "secret")
	auth := newTestAuth(t, repo)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	form := url.Values{"email": {"user@example.com"}, "password": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "http://app.test", rec.Header().Get("Location"))

	var sessionCookieVal string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookieVal = c.Value
		}
	}
	assert.NotEmpty(t, sessionCookieVal)

	composite := auth.(*AuthComposite)
	user, err := composite.validateSessionToken(req.Context(), sessionCookieVal)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", user.Email)
}

func TestSelfLogin_WrongPassword(t *testing.T) {
	repo := newRepository(t)
	createUserWithPassword(t, repo, "user@example.com", "secret")
	auth := newTestAuth(t, repo)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	form := url.Values{"email": {"user@example.com"}, "password": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid email or password")
	for _, c := range rec.Result().Cookies() {
		assert.NotEqual(t, sessionCookieName, c.Name)
	}
}

func TestSelfLogin_UnknownUser(t *testing.T) {
	repo := newRepository(t)
	auth := newTestAuth(t, repo)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	form := url.Values{"email": {"nobody@example.com"}, "password": {"whatever"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid email or password")
}

func TestMiddleware_SessionCookie(t *testing.T) {
	repo := newRepository(t)
	user := createUserWithPassword(t, repo, "user@example.com", "secret")
	auth := newTestAuth(t, repo)
	composite := auth.(*AuthComposite)

	token, err := composite.generateSessionToken(user.ID)
	require.NoError(t, err)

	var gotUser User
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, gotOK)
	assert.Equal(t, "user@example.com", gotUser.Email)
}

func TestMiddleware_NoCookiesRedirectsToLogin(t *testing.T) {
	repo := newRepository(t)
	auth := newTestAuth(t, repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	rec := httptest.NewRecorder()

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestLoginPage_CreatedUserRedirectsToRoot(t *testing.T) {
	repo := newRepository(t)
	user := createUserWithPassword(t, repo, "user@example.com", "secret")
	auth := newTestAuth(t, repo)
	composite := auth.(*AuthComposite)

	token, err := composite.generateSessionToken(user.ID)
	require.NoError(t, err)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "http://app.test", rec.Header().Get("Location"))
}

func TestLoginPage_RendersForm(t *testing.T) {
	repo := newRepository(t)
	auth := newTestAuth(t, repo)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sign in")
	assert.Contains(t, rec.Body.String(), `name="email"`)
	assert.Contains(t, rec.Body.String(), `name="password"`)
	assert.NotContains(t, rec.Body.String(), "Sign in with SSO")
}

func TestLoginPage_RendersSSOButtonWhenOIDCConfigured(t *testing.T) {
	repo := newRepository(t)

	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)

	hmacKey := sha256.Sum256([]byte("test-secret-key"))
	auth := &AuthComposite{
		oidc: &AuthOIDC{
			authCodeURL:      "https://sso.example.com/auth",
			loginURL:         "/login",
			callbackEndpoint: "/oauth2/callback",
		},
		repo:                 repo,
		secretKey:            hmacKey[:],
		baseURL:              baseURL,
		loginEndpoint:        NewEndpoint(http.MethodGet, baseURL.Path, "/login"),
		authenticateEndpoint: NewEndpoint(http.MethodPost, baseURL.Path, "/login"),
		logoutEndpoint:       NewEndpoint(http.MethodGet, baseURL.Path, "/logout"),
	}

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sign in with SSO")
	assert.Contains(t, rec.Body.String(), "https://sso.example.com/auth")
	assert.Contains(t, rec.Body.String(), `name="email"`)
}

func TestLogout_ClearsSessionCookie(t *testing.T) {
	repo := newRepository(t)
	user := createUserWithPassword(t, repo, "user@example.com", "secret")
	auth := newTestAuth(t, repo)
	composite := auth.(*AuthComposite)

	token, err := composite.generateSessionToken(user.ID)
	require.NoError(t, err)

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			assert.Equal(t, "", c.Value)
			assert.Equal(t, -1, c.MaxAge)
		}
	}
}
