package projektovemeeting

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	g "maragu.dev/gomponents"
	co "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"
)

const sessionCookieName = "session_token"

type AuthOIDC struct {
	repo              Repository
	verifier          *oidc.IDTokenVerifier
	provider          *oidc.Provider
	idTokenCookieName string
	refreshCookieName string
	baseURL           *url.URL
	issuer            string

	callbackEndpoint string
	endSessionURL    string
	oauth2Config     oauth2.Config
	authCodeURL      string

	loginURL string
}

func NewAuthOIDC(repo Repository, config ConfigOIDC, baseURL *url.URL, loginURL string) (*AuthOIDC, error) {
	provider, err := oidc.NewProvider(context.Background(), config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("when discovering oidc provider %q: %w", config.Issuer, err)
	}

	callbackURL := baseURL.JoinPath(config.CallbackEndpoint)
	if err != nil {
		return nil, fmt.Errorf("when parsing callback url %q: %w", config.CallbackEndpoint, err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  callbackURL.String(),
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}

	refreshTokenCookieName := config.RefreshTokenCookieName
	if refreshTokenCookieName == "" {
		refreshTokenCookieName = config.IDTokenCookieName + "_refresh"
	}

	return &AuthOIDC{
		repo:              repo,
		verifier:          provider.Verifier(&oidc.Config{ClientID: config.ClientID}),
		provider:          provider,
		idTokenCookieName: config.IDTokenCookieName,
		refreshCookieName: refreshTokenCookieName,
		callbackEndpoint:  callbackURL.Path,
		oauth2Config:      oauth2Config,
		authCodeURL:       oauth2Config.AuthCodeURL(""),
		baseURL:           baseURL,
		endSessionURL:     config.EndSessionURL,
		issuer:            config.Issuer,
		loginURL:          loginURL,
	}, nil
}

type oidcClaims struct {
	Email string `json:"email"`
}

func (a AuthOIDC) authenticate(ctx context.Context, tokens *Tokens) (User, error) {
	user, err := a.authenticateWithIDToken(ctx, tokens.ID)
	if err == nil {
		return user, nil
	}

	if tokens.Refresh == "" {
		return User{}, err
	}

	refreshedIDToken, rotatedRefreshToken, refreshErr := a.refresh(ctx, tokens.Refresh)
	if refreshErr != nil {
		slog.Debug("Token refresh failed", "error", refreshErr.Error())
		return User{}, fmt.Errorf("when refreshing session (id token verification failed: %v): %w", err, refreshErr)
	}

	user, err = a.authenticateWithIDToken(ctx, refreshedIDToken)
	if err != nil {
		return User{}, err
	}

	tokens.ID = refreshedIDToken
	tokens.Refresh = rotatedRefreshToken

	return user, nil
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

		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			WriteError(w, "Missing ID Token", http.StatusUnauthorized, nil)
			return
		}

		idToken, err := a.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			WriteError(w, "Token verification failed", http.StatusUnauthorized, err)
			return
		}

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
		http.Redirect(w, r, a.baseURL.String(), http.StatusFound)
	})
}

func (a AuthOIDC) logout(w http.ResponseWriter, r *http.Request) {
	idTokenHint := ""
	idTokenCookie, err := r.Cookie(a.idTokenCookieName)
	if err != nil {
		http.Redirect(w, r, a.loginURL, http.StatusFound)
		return
	}

	idTokenHint = idTokenCookie.Value
	a.clearCookies(w)
	http.Redirect(w, r, fmt.Sprintf("%s?id_token_hint=%s&post_logout_redirect_uri=%s", a.endSessionURL, idTokenHint, url.QueryEscape(a.loginURL)), http.StatusFound)
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

func (a AuthOIDC) setCookies(w http.ResponseWriter, result Tokens) {
	if result.ID != "" {
		setAuthCookie(w, a.idTokenCookieName, result.ID)
	}
	if result.Refresh != "" {
		setAuthCookie(w, a.refreshCookieName, result.Refresh)
	}
}

func (a AuthOIDC) clearAllCookies(w http.ResponseWriter) {
	a.clearCookies(w)
}

func (a *AuthComposite) Authenticate(ctx context.Context, tokens *Tokens) (User, error) {
	if tokens.Session != "" {
		user, err := a.validateSessionToken(ctx, tokens.Session)
		if err != nil {
			return User{}, fmt.Errorf("when validating session token: %w", err)
		}

		return user, nil
	}

	if a.oidc == nil {
		return User{}, fmt.Errorf("oidc not configured")
	}

	if tokens.ID == "" {
		return User{}, fmt.Errorf("id token is empty")
	}

	user, err := a.oidc.authenticate(ctx, tokens)
	if err != nil {
		return User{}, fmt.Errorf("when validating session token: %w", err)
	}

	return user, nil

}

// AuthComposite combines optional OIDC auth with self-login (JWT session) auth.
type AuthComposite struct {
	oidc                 *AuthOIDC
	repo                 Repository
	secretKey            []byte
	baseURL              *url.URL
	loginEndpoint        Endpoint
	authenticateEndpoint Endpoint
	logoutEndpoint       Endpoint
}

func NewAuth(repo Repository, oidcConfig *ConfigOIDC, secretKey string, endpointLogin, endpointLogout Endpoint, baseURL *url.URL) (Auth, error) {
	loginURL, err := url.JoinPath(baseURL.String(), endpointLogin.Path())
	if err != nil {
		return nil, fmt.Errorf("when constructing login url: %w", err)
	}
	_, err = url.JoinPath(baseURL.String(), endpointLogout.Path())
	if err != nil {
		return nil, fmt.Errorf("when constructing logout url: %w", err)
	}

	var oidcAuth *AuthOIDC
	if oidcConfig != nil && oidcConfig.Issuer != "" {
		var err error
		oidcAuth, err = NewAuthOIDC(repo, *oidcConfig, baseURL, loginURL)
		if err != nil {
			return nil, fmt.Errorf("when constructing OIDC auth: %w", err)
		}
	}

	hmacKey := sha256.Sum256([]byte(secretKey))

	return &AuthComposite{
		oidc:                 oidcAuth,
		repo:                 repo,
		secretKey:            hmacKey[:],
		baseURL:              baseURL,
		loginEndpoint:        endpointLogin,
		logoutEndpoint:       endpointLogout,
		authenticateEndpoint: NewEndpoint(http.MethodPost, baseURL.Path, endpointLogin.PathRaw()),
	}, nil
}

func (a *AuthComposite) generateSessionToken(userID int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})
	return token.SignedString(a.secretKey)
}

func (a *AuthComposite) validateSessionToken(ctx context.Context, tokenStr string) (User, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secretKey, nil
	})
	if err != nil {
		return User{}, fmt.Errorf("invalid session token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return User{}, fmt.Errorf("invalid session token claims")
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		return User{}, fmt.Errorf("invalid user_id in session token")
	}

	user, err := a.repo.GetUserByID(ctx, int(userIDFloat))
	if err != nil {
		return User{}, fmt.Errorf("user not found for session token: %w", err)
	}

	return user, nil
}

func (a *AuthComposite) authenticate(w http.ResponseWriter, r *http.Request) (User, Tokens, error) {
	tokens := Tokens{}

	// Check self-login session cookie first
	if sessionCookie, err := r.Cookie(sessionCookieName); err == nil {
		tokens.Session = sessionCookie.Value
	}

	// Check OIDC cookies if configured
	if a.oidc != nil {
		idTokenCookie, err := r.Cookie(a.oidc.idTokenCookieName)
		if err == nil {
			tokens.ID = idTokenCookie.Value
		}

		refreshCookie, err := r.Cookie(a.oidc.refreshCookieName)
		if err == nil {
			tokens.Refresh = refreshCookie.Value
		}
	}

	user, err := a.Authenticate(r.Context(), &tokens)
	if err != nil {
		return User{}, tokens, fmt.Errorf("not authenticated")
	}

	return user, tokens, nil
}

func (a *AuthComposite) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, tokens, err := a.authenticate(w, r)
		if err != nil {
			clearSessionCookie(w)
			if a.oidc != nil && tokens.ID != "" {
				a.oidc.logout(w, r)
				return
			}
			// Not authenticated — redirect to login
			slog.Debug("No authentication found, redirecting to login")
			http.Redirect(w, r, a.loginEndpoint.Path(), http.StatusFound)
			return
		}
		if a.oidc != nil {
			a.oidc.setCookies(w, tokens)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUser, u)))
	})
}

func (a *AuthComposite) RegisterRoutes(m *http.ServeMux) {
	// OIDC routes (callback + OIDC-specific logout)
	if a.oidc != nil {
		a.oidc.RegisterRoutes(m)
	}

	// Self-login endpoint
	m.HandleFunc(a.loginEndpoint.Pattern(), func(w http.ResponseWriter, r *http.Request) {
		_, _, err := a.authenticate(w, r)
		if err == nil {
			http.Redirect(w, r, a.baseURL.String(), http.StatusFound)
			return
		}

		outputcss := a.baseURL.JoinPath("/static/css/output.css")

		page := co.HTML5(
			co.HTML5Props{
				Title: "Sign in",
				Head: []g.Node{
					h.Link(h.Rel("stylesheet"), h.Href(outputcss.Path)),
				},
				Body: []g.Node{
					h.Div(
						h.Class("min-h-screen flex items-center justify-center px-4"),
						h.Div(
							h.Class("w-full max-w-md"),
							h.Div(
								h.Class("text-center mb-8"),
								h.H1(h.Class("text-3xl font-bold text-gray-900"), g.Text("Meetings -> Projektove")),
								h.P(h.Class("mt-2 text-sm text-gray-500"), g.Text("Sign in to your account to continue")),
							),
							h.Div(
								h.Class("bg-white shadow rounded-lg p-8"),
								h.Form(
									h.Method(a.authenticateEndpoint.method),
									h.Action(a.authenticateEndpoint.Path()),
									h.Class("space-y-6"),
									h.Div(
										h.Label(h.For("email"), h.Class("block text-sm font-medium text-gray-700 mb-1"), g.Text("Email")),
										h.Input(
											h.ID("email"),
											h.Name("email"),
											h.Type("email"),
											h.Required(),
											h.AutoComplete("email"),
											h.Class("w-full px-3 py-2 border border-gray-300 rounded-md bg-white text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-gray-800 focus:border-transparent"),
											h.Placeholder("you@example.com"),
										),
									),
									h.Div(
										h.Label(h.For("password"), h.Class("block text-sm font-medium text-gray-700 mb-1"), g.Text("Password")),
										h.Input(
											h.ID("password"),
											h.Name("password"),
											h.Type("password"),
											h.Required(),
											h.AutoComplete("current-password"),
											h.Class("w-full px-3 py-2 border border-gray-300 rounded-md bg-white text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-gray-800 focus:border-transparent"),
										),
									),
									h.Button(
										h.Type("submit"),
										h.Class("w-full rounded-md bg-gray-800 hover:bg-gray-900 text-white font-medium py-2 px-4 transition-colors cursor-pointer"),
										g.Text("Sign in"),
									),
								),
								g.Iff(a.oidc != nil, func() g.Node {
									return g.Group([]g.Node{
										h.Div(
											h.Class("my-6 relative"),
											h.Div(h.Class("absolute inset-0 flex items-center"), h.Div(h.Class("w-full border-t border-gray-200"))),
											h.Div(h.Class("relative flex justify-center text-sm"), h.Span(h.Class("bg-white px-3 text-gray-500"), g.Text("or"))),
										),
										h.A(
											h.Href(a.oidc.authCodeURL),
											h.Class("w-full inline-flex items-center justify-center gap-2 rounded-md border border-gray-300 bg-white hover:bg-gray-50 text-gray-700 font-medium py-2 px-4 transition-colors cursor-pointer"),
											g.Text("Sign in with SSO"),
										),
									})
								}),
							),
							h.P(h.Class("mt-6 text-center text-xs text-gray-400"), g.Text("Meetings to Issues")),
						),
					),
				},
			},
		)

		page.Render(w)
	})

	m.HandleFunc(a.authenticateEndpoint.Pattern(), func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			WriteError(w, "Invalid form submission", http.StatusBadRequest, err)
			return
		}

		email := strings.TrimSpace(r.Form.Get("email"))
		password := r.Form.Get("password")

		user, err := a.repo.GetUser(r.Context(), email)
		if err != nil || user.PasswordHash == nil {
			slog.Error("User not found", "email", email)
			WriteError(w, "Invalid email or password", http.StatusBadRequest, err)
			return
		}

		if err := bcryptCompare([]byte(password), []byte(*user.PasswordHash)); err != nil {
			slog.Error("Bad password", "email", email)
			WriteError(w, "Invalid email or password", http.StatusBadRequest, err)
			return
		}

		token, err := a.generateSessionToken(user.ID)
		if err != nil {
			WriteError(w, "Failed to create session", http.StatusInternalServerError, err)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			MaxAge:   86400,
		})

		http.Redirect(w, r, a.baseURL.String(), http.StatusFound)
	})

	// Self-login logout (clears session cookie)
	m.HandleFunc(a.logoutEndpoint.Pattern(), func(w http.ResponseWriter, r *http.Request) {
		clearSessionCookie(w)
		if a.oidc != nil {
			a.oidc.logout(w, r)
			return
		}
		http.Redirect(w, r, a.loginEndpoint.Path(), http.StatusFound)
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
	})
}

func bcryptCompare(password, hash []byte) error {
	return bcrypt.CompareHashAndPassword(hash, password)
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("when hashing password: %w", err)
	}
	return string(hash), nil
}
