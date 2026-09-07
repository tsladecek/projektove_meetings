package projektovemeeting

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func createUser(t *testing.T, repo Repository, email string) User {
	t.Helper()
	_, err := repo.StoreUser(t.Context(), UserCreate{Email: email})
	require.NoError(t, err)
	user, err := repo.GetUser(t.Context(), email)
	require.NoError(t, err)
	return user
}

type testRSAKeys struct {
	mu  sync.Mutex
	key *rsa.PrivateKey
}

var testsKeys testRSAKeys

func (k *testRSAKeys) privateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key == nil {
		pk, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		k.key = pk
	}
	return k.key
}

func authVerifier(t *testing.T, clientID string) (*oidc.IDTokenVerifier, string) {
	t.Helper()
	key := testsKeys.privateKey(t)

	jwks := map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"kid": "test-kid",
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(bigEndian(key.E)),
	}}}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	realIssuer := srv.URL

	providerJSON := map[string]any{
		"issuer":                                realIssuer,
		"jwks_uri":                              realIssuer + "/keys",
		"token_endpoint":                        realIssuer + "/token",
		"response_types_supported":              []string{"code", "id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, providerJSON)
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, jwks)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		rawIDToken := signToken(t, realIssuer, testClientID, testEmail, testKid)
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != testAuthCode {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(t, w, map[string]any{"error": "invalid_grant"})
				return
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "access-token",
				"token_type":    "Bearer",
				"expires_in":    300,
				"refresh_token": testInitialRefreshToken,
				"id_token":      rawIDToken,
			})
		case "refresh_token":
			if r.Form.Get("refresh_token") != testRefreshToken {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(t, w, map[string]any{"error": "invalid_grant"})
				return
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "new-access-token",
				"token_type":    "Bearer",
				"expires_in":    300,
				"refresh_token": testRotatedRefreshToken,
				"id_token":      rawIDToken,
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(t, w, map[string]any{"error": "unsupported_grant_type"})
		}
	})

	provider, err := oidc.NewProvider(context.Background(), realIssuer)
	require.NoError(t, err)

	return provider.Verifier(&oidc.Config{ClientID: clientID}), realIssuer
}

func signToken(t *testing.T, issuer, clientID, email, kid string) string {
	return signTokenExp(t, issuer, clientID, email, kid, time.Now().Add(time.Hour))
}

func signTokenExp(t *testing.T, issuer, clientID, email, kid string, exp time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   issuer,
		"aud":   clientID,
		"sub":   email,
		"email": email,
		"exp":   exp.Unix(),
		"iat":   time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(testsKeys.privateKey(t))
	require.NoError(t, err)
	return signed
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
}

func bigEndian(n int) []byte {
	bytes := make([]byte, 0)
	for n > 0 {
		bytes = append([]byte{byte(n & 0xff)}, bytes...)
		n >>= 8
	}
	return bytes
}

const (
	testClientID            = "client-id"
	testEmail               = "user@example.com"
	testKid                 = "test-kid"
	testAuthCode            = "auth-code"
	testRefreshToken        = "valid-refresh-token"
	testRotatedRefreshToken = "rotated-refresh-token"
	testInitialRefreshToken = "initial-refresh"
)

func TestAuthenticate_Success(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	verifier, issuer := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier}

	token := signToken(t, issuer, testClientID, testEmail, testKid)
	user, _, err := auth.Authenticate(t.Context(), token, "")
	require.NoError(t, err)
	assert.Equal(t, testEmail, user.Email)
	assert.NotZero(t, user.ID)
}

func TestAuthenticate_AutoProvision(t *testing.T) {
	newEmail := "new@example.com"
	repo := newRepository(t)
	verifier, issuer := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier}

	token := signToken(t, issuer, testClientID, newEmail, testKid)
	user, _, err := auth.Authenticate(t.Context(), token, "")
	require.NoError(t, err)
	assert.Equal(t, newEmail, user.Email)
	assert.NotZero(t, user.ID)

	stored, err := repo.GetUser(t.Context(), newEmail)
	require.NoError(t, err)
	assert.Equal(t, "", stored.ProjektoveToken)
	assert.Empty(t, stored.LLMModels)
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	repo := newRepository(t)
	verifier, _ := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier}

	_, _, err := auth.Authenticate(t.Context(), "not-a-token", "")
	require.Error(t, err)
}

func TestAuthenticate_WrongAudience(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	verifier, issuer := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier}

	token := signToken(t, issuer, "other-client", testEmail, testKid)
	_, _, err := auth.Authenticate(t.Context(), token, "")
	require.Error(t, err)
}

func TestMiddlewareAuth_ValidCookie(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	verifier, issuer := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier, idTokenCookieName: "token", refreshCookieName: "refresh"}

	token := signToken(t, issuer, testClientID, testEmail, testKid)

	var gotUser User
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	rec := httptest.NewRecorder()

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, gotOK)
	assert.Equal(t, testEmail, gotUser.Email)
}

func TestMiddlewareAuth_MissingCookie(t *testing.T) {
	auth := AuthOIDC{loginURL: "/login"}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	rec := httptest.NewRecorder()

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestMiddlewareAuth_InvalidToken(t *testing.T) {
	repo := newRepository(t)
	verifier, _ := authVerifier(t, testClientID)
	auth := AuthOIDC{repo: repo, verifier: verifier, idTokenCookieName: "token", refreshCookieName: "refresh", loginURL: "/login"}

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "garbage"})
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func authOIDCWithTokenEndpoint(t *testing.T, repo Repository) AuthOIDC {
	t.Helper()
	verifier, issuer := authVerifier(t, testClientID)
	return AuthOIDC{
		repo:              repo,
		verifier:          verifier,
		idTokenCookieName: "token",
		refreshCookieName: "refresh",
		loginURL:          "/login",
		oauth2Config: oauth2.Config{
			ClientID:     testClientID,
			ClientSecret: "secret",
			Endpoint:     oauth2.Endpoint{TokenURL: issuer + "/token"},
		},
	}
}

func TestAuthenticate_RefreshExpiredIDToken(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	auth := authOIDCWithTokenEndpoint(t, repo)

	expired := signTokenExp(t, "", testClientID, testEmail, testKid, time.Now().Add(-time.Hour))
	user, result, err := auth.Authenticate(t.Context(), expired, testRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, testEmail, user.Email)
	assert.NotEmpty(t, result.IDToken)
	assert.Equal(t, testRotatedRefreshToken, result.RefreshToken)

	_, err = auth.verifier.Verify(t.Context(), result.IDToken)
	require.NoError(t, err)
}

func TestAuthenticate_RefreshFails(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	auth := authOIDCWithTokenEndpoint(t, repo)

	expired := signTokenExp(t, "", testClientID, testEmail, testKid, time.Now().Add(-time.Hour))
	_, _, err := auth.Authenticate(t.Context(), expired, "bad-refresh-token")
	require.Error(t, err)
}

func TestMiddlewareAuth_RefreshesExpiredToken(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	auth := authOIDCWithTokenEndpoint(t, repo)

	expired := signTokenExp(t, "", testClientID, testEmail, testKid, time.Now().Add(-time.Hour))

	var gotUser User
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: expired})
	req.AddCookie(&http.Cookie{Name: "refresh", Value: testRefreshToken})
	rec := httptest.NewRecorder()

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, gotOK)
	assert.Equal(t, testEmail, gotUser.Email)

	var idCookieVal, refreshCookieVal string
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "token":
			idCookieVal = c.Value
		case "refresh":
			refreshCookieVal = c.Value
		}
	}
	assert.NotEmpty(t, idCookieVal)
	assert.NotEqual(t, expired, idCookieVal)
	assert.Equal(t, testRotatedRefreshToken, refreshCookieVal)
}

func TestMiddlewareAuth_RefreshFails(t *testing.T) {
	repo := newRepository(t)
	createUser(t, repo, testEmail)
	auth := authOIDCWithTokenEndpoint(t, repo)

	expired := signTokenExp(t, "", testClientID, testEmail, testKid, time.Now().Add(-time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/prompts", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: expired})
	req.AddCookie(&http.Cookie{Name: "refresh", Value: "bad-refresh-token"})
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	auth.Middleware(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
	for _, c := range rec.Result().Cookies() {
		assert.Equal(t, "", c.Value)
	}
}

func TestCallback_SetsRefreshCookie(t *testing.T) {
	repo := newRepository(t)
	verifier, issuer := authVerifier(t, testClientID)
	auth := AuthOIDC{
		repo:              repo,
		verifier:          verifier,
		idTokenCookieName: "token",
		refreshCookieName: "refresh",
		callbackEndpoint:  "/oauth2/callback",
		logoutEndoint:     "/oauth2/logout",
		baseURL:           "http://app.test",
		oauth2Config: oauth2.Config{
			ClientID:     testClientID,
			ClientSecret: "secret",
			Endpoint:     oauth2.Endpoint{TokenURL: issuer + "/token"},
		},
	}

	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, auth.callbackEndpoint+"?code="+testAuthCode, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)

	var idCookieVal, refreshCookieVal string
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "token":
			idCookieVal = c.Value
		case "refresh":
			refreshCookieVal = c.Value
		}
	}
	assert.NotEmpty(t, idCookieVal)
	assert.Equal(t, testInitialRefreshToken, refreshCookieVal)
}
