package projektovemeeting

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type AuthOIDC struct {
	repo              Repository
	verifier          *oidc.IDTokenVerifier
	provider          *oidc.Provider
	idTokenCookieName string
	refreshCookieName string
	baseURL           string
	issuer            string

	// inferred
	callbackEndpoint string
	logoutEndoint    string
	oauth2Config     oauth2.Config
	loginURL         string
}

func NewAuth(repo Repository, config ConfigOIDC, baseURLRaw string) (Auth, error) {
	provider, err := oidc.NewProvider(context.Background(), config.Issuer)
	if err != nil {
		return AuthOIDC{}, fmt.Errorf("when discovering oidc provider %q: %w", config.Issuer, err)
	}

	baseURL, err := url.Parse(baseURLRaw)
	if err != nil {
		return nil, fmt.Errorf("when parsing base url %q: %w", baseURLRaw, err)
	}

	callbackURL, err := url.JoinPath(baseURL.String(), config.CallbackEndpoint)
	if err != nil {
		return nil, fmt.Errorf("when parsing callback url %q: %w", config.CallbackEndpoint, err)
	}

	// Configure an OpenID Connect aware OAuth2 client.
	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  callbackURL,

		// Discovery returns the OAuth2 endpoints.
		Endpoint: provider.Endpoint(),

		// "openid" is a required scope for OpenID Connect flows.
		Scopes: []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}

	refreshTokenCookieName := config.RefreshTokenCookieName
	if refreshTokenCookieName == "" {
		refreshTokenCookieName = config.IDTokenCookieName + "_refresh"
	}

	return AuthOIDC{
		repo:              repo,
		verifier:          provider.Verifier(&oidc.Config{ClientID: config.ClientID}),
		provider:          provider,
		idTokenCookieName: config.IDTokenCookieName,
		refreshCookieName: refreshTokenCookieName,
		callbackEndpoint:  config.CallbackEndpoint,
		oauth2Config:      oauth2Config,
		loginURL:          oauth2Config.AuthCodeURL(""),
		baseURL:           baseURL.String(),
		logoutEndoint:     config.LogoutEndpoint,
		issuer:            config.Issuer,
	}, nil
}

type oidcClaims struct {
	Email string `json:"email"`
}

type AuthTokenResult struct {
	IDToken      string
	RefreshToken string
}

func (a AuthOIDC) Authenticate(ctx context.Context, idToken, refreshToken string) (User, AuthTokenResult, error) {
	user, err := a.authenticateWithIDToken(ctx, idToken)
	if err == nil {
		return user, AuthTokenResult{}, nil
	}

	if refreshToken == "" {
		return User{}, AuthTokenResult{}, err
	}

	refreshedIDToken, rotatedRefreshToken, refreshErr := a.refresh(ctx, refreshToken)
	if refreshErr != nil {
		slog.Debug("Token refresh failed", "error", refreshErr.Error())
		return User{}, AuthTokenResult{}, fmt.Errorf("when refreshing session (id token verification failed: %v): %w", err, refreshErr)
	}

	user, err = a.authenticateWithIDToken(ctx, refreshedIDToken)
	if err != nil {
		return User{}, AuthTokenResult{}, err
	}

	return user, AuthTokenResult{IDToken: refreshedIDToken, RefreshToken: rotatedRefreshToken}, nil
}

func (a AuthOIDC) authenticateWithIDToken(ctx context.Context, token string) (User, error) {
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return User{}, fmt.Errorf("when verifying id token: %w", err)
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return User{}, fmt.Errorf("when parsing id token claims: %w", err)
	}

	if claims.Email == "" {
		return User{}, fmt.Errorf("id token does not contain an email claim")
	}

	user, err := a.repo.GetUser(ctx, claims.Email)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return User{}, fmt.Errorf("when fetching user: %w", err)
	}

	slog.Debug("User not found, provisioning", "email", claims.Email)
	if _, err := a.repo.StoreUser(ctx, UserCreate{Email: claims.Email}); err != nil {
		return User{}, fmt.Errorf("when provisioning user: %w", err)
	}

	user, err = a.repo.GetUser(ctx, claims.Email)
	if err != nil {
		return User{}, fmt.Errorf("when fetching provisioned user: %w", err)
	}

	return user, nil
}

func (a AuthOIDC) refresh(ctx context.Context, refreshToken string) (string, string, error) {
	src := a.oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", "", fmt.Errorf("when exchanging refresh token: %w", err)
	}

	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		return "", "", errors.New("token refresh response is missing the id token")
	}

	return rawIDToken, tok.RefreshToken, nil
}

type ctxKey string

const ctxKeyUser ctxKey = "user"

func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(User)
	return u, ok
}

func (a AuthOIDC) RegisterRoutes(m *http.ServeMux) {
	m.HandleFunc(http.MethodGet+" "+a.callbackEndpoint, func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		oauth2Token, err := a.oauth2Config.Exchange(ctx, r.URL.Query().Get("code"))
		if err != nil {
			WriteError(w, "Failed to exchange tokens", http.StatusUnauthorized, err)
			return
		}

		// Extract the ID Token from OAuth2 token.
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			WriteError(w, "Missing ID Token", http.StatusUnauthorized, nil)
			return
		}

		// Parse and verify ID Token payload.
		idToken, err := a.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			WriteError(w, "Token verification failed", http.StatusUnauthorized, err)
			return
		}

		// Extract custom claims
		var claims struct {
			Email string `json:"email"`
		}
		if err := idToken.Claims(&claims); err != nil {
			WriteError(w, "Failed to extract tokens", http.StatusUnauthorized, err)
			return
		}

		setAuthCookie(w, a.idTokenCookieName, rawIDToken)
		if oauth2Token.RefreshToken != "" {
			setAuthCookie(w, a.refreshCookieName, oauth2Token.RefreshToken)
		}
		slog.Debug("Authentication Successful")
		http.Redirect(w, r, a.baseURL, http.StatusFound)
	})

	m.HandleFunc(http.MethodGet+" "+a.logoutEndoint, func(w http.ResponseWriter, r *http.Request) {
		idTokenHint := ""
		idTokenCookie, err := r.Cookie(a.idTokenCookieName)
		if err != nil {
			slog.Error("No ID Token Found during logout")
			http.Redirect(w, r, a.baseURL, http.StatusFound)
			return
		}

		idTokenHint = idTokenCookie.Value
		a.clearCookies(w)
		http.Redirect(w, r, fmt.Sprintf("%s/protocol/openid-connect/logout?id_token_hint=%s&post_logout_redirect_uri=%s", a.issuer, idTokenHint, url.QueryEscape(a.loginURL)), http.StatusFound)
	})
}

func setAuthCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
	})
}

func (a AuthOIDC) clearCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: a.idTokenCookieName, Path: "/", Value: "", MaxAge: -1, HttpOnly: true, Secure: true})
	http.SetCookie(w, &http.Cookie{Name: a.refreshCookieName, Path: "/", Value: "", MaxAge: -1, HttpOnly: true, Secure: true})
}

func (a AuthOIDC) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idTokenCookie, idErr := r.Cookie(a.idTokenCookieName)
		refreshCookie, refreshErr := r.Cookie(a.refreshCookieName)
		if idErr != nil && refreshErr != nil {
			slog.Debug("No authentication cookies found")
			http.Redirect(w, r, a.loginURL, http.StatusFound)
			return
		}

		idToken := ""
		if idErr == nil {
			idToken = idTokenCookie.Value
		}
		refreshToken := ""
		if refreshErr == nil {
			refreshToken = refreshCookie.Value
		}

		user, result, err := a.Authenticate(r.Context(), idToken, refreshToken)
		if err != nil {
			slog.Debug("Authentication failed", "error", err.Error())
			a.clearCookies(w)
			http.Redirect(w, r, a.loginURL, http.StatusFound)
			return
		}

		if result.IDToken != "" {
			setAuthCookie(w, a.idTokenCookieName, result.IDToken)
		}
		if result.RefreshToken != "" {
			setAuthCookie(w, a.refreshCookieName, result.RefreshToken)
		}

		slog.Debug("Authentication successful")

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}
