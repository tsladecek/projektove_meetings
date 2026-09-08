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
)

const sessionCookieName = "session_token"

type AuthOIDC struct {
	repo              Repository
	verifier          *oidc.IDTokenVerifier
	provider          *oidc.Provider
	idTokenCookieName string
	refreshCookieName string
	baseURL           string
	issuer            string

	callbackEndpoint string
	logoutEndoint    string
	oauth2Config     oauth2.Config
	loginURL         string
}

func NewAuthOIDC(repo Repository, config ConfigOIDC, baseURLRaw string) (*AuthOIDC, error) {
	provider, err := oidc.NewProvider(context.Background(), config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("when discovering oidc provider %q: %w", config.Issuer, err)
	}

	baseURL, err := url.Parse(baseURLRaw)
	if err != nil {
		return nil, fmt.Errorf("when parsing base url %q: %w", baseURLRaw, err)
	}

	callbackURL, err := url.JoinPath(baseURL.String(), config.CallbackEndpoint)
	if err != nil {
		return nil, fmt.Errorf("when parsing callback url %q: %w", config.CallbackEndpoint, err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  callbackURL,
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

func (a AuthOIDC) authenticate(ctx context.Context, idToken, refreshToken string) (User, AuthTokenResult, error) {
	return a.Authenticate(ctx, idToken, refreshToken)
}

func (a AuthOIDC) setCookies(w http.ResponseWriter, result AuthTokenResult) {
	if result.IDToken != "" {
		setAuthCookie(w, a.idTokenCookieName, result.IDToken)
	}
	if result.RefreshToken != "" {
		setAuthCookie(w, a.refreshCookieName, result.RefreshToken)
	}
}

func (a AuthOIDC) clearAllCookies(w http.ResponseWriter) {
	a.clearCookies(w)
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

func (a *AuthComposite) Authenticate(ctx context.Context, idToken, refreshToken string) (User, AuthTokenResult, error) {
	if a.oidc == nil {
		return User{}, AuthTokenResult{}, errors.New("oidc authentication is not configured")
	}
	return a.oidc.Authenticate(ctx, idToken, refreshToken)
}

// AuthComposite combines optional OIDC auth with self-login (JWT session) auth.
type AuthComposite struct {
	oidc          *AuthOIDC
	repo          Repository
	secretKey     []byte
	baseURL       string
	loginEndpoint string
}

func NewAuth(repo Repository, oidcConfig *ConfigOIDC, authConfig ConfigAuth, baseURL string) (Auth, error) {
	var oidcAuth *AuthOIDC
	if oidcConfig != nil && oidcConfig.Issuer != "" {
		var err error
		oidcAuth, err = NewAuthOIDC(repo, *oidcConfig, baseURL)
		if err != nil {
			return nil, fmt.Errorf("when constructing OIDC auth: %w", err)
		}
	}

	hmacKey := sha256.Sum256([]byte(authConfig.SecretKey))

	return &AuthComposite{
		oidc:          oidcAuth,
		repo:          repo,
		secretKey:     hmacKey[:],
		baseURL:       baseURL,
		loginEndpoint: "/login",
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

func (a *AuthComposite) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Check self-login session cookie first
		if sessionCookie, err := r.Cookie(sessionCookieName); err == nil {
			user, err := a.validateSessionToken(r.Context(), sessionCookie.Value)
			if err == nil {
				// Authenticated via self-login
				if path == a.loginEndpoint {
					http.Redirect(w, r, a.baseURL, http.StatusFound)
					return
				}
				next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
				return
			}
			// Invalid session cookie — clear it
			clearSessionCookie(w)
		}

		// Check OIDC cookies if configured
		if a.oidc != nil {
			idTokenCookie, idErr := r.Cookie(a.oidc.idTokenCookieName)
			refreshCookie, refreshErr := r.Cookie(a.oidc.refreshCookieName)
			if idErr == nil || refreshErr == nil {
				idToken := ""
				if idErr == nil {
					idToken = idTokenCookie.Value
				}
				refreshToken := ""
				if refreshErr == nil {
					refreshToken = refreshCookie.Value
				}

				user, result, err := a.oidc.authenticate(r.Context(), idToken, refreshToken)
				if err == nil {
					// Authenticated via OIDC
					if path == a.loginEndpoint {
						http.Redirect(w, r, a.baseURL, http.StatusFound)
						return
					}
					a.oidc.setCookies(w, result)
					next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
					return
				}
				// Invalid OIDC tokens — clear them
				a.oidc.clearAllCookies(w)
			}
		}

		// Not authenticated — redirect to login
		slog.Debug("No authentication found, redirecting to login")
		http.Redirect(w, r, a.loginEndpoint, http.StatusFound)
	})
}

func (a *AuthComposite) RegisterRoutes(m *http.ServeMux) {
	// OIDC routes (callback + OIDC-specific logout)
	if a.oidc != nil {
		a.oidc.RegisterRoutes(m)
	}

	// Self-login endpoint
	m.HandleFunc(http.MethodGet+" "+a.loginEndpoint, func(w http.ResponseWriter, r *http.Request) {
		// If already authenticated (either method), redirect to root
		if sessionCookie, err := r.Cookie(sessionCookieName); err == nil {
			if _, err := a.validateSessionToken(r.Context(), sessionCookie.Value); err == nil {
				http.Redirect(w, r, a.baseURL, http.StatusFound)
				return
			}
		}
		if a.oidc != nil {
			idTokenCookie, idErr := r.Cookie(a.oidc.idTokenCookieName)
			refreshCookie, refreshErr := r.Cookie(a.oidc.refreshCookieName)
			if idErr == nil || refreshErr == nil {
				idToken := ""
				if idErr == nil {
					idToken = idTokenCookie.Value
				}
				refreshToken := ""
				if refreshErr == nil {
					refreshToken = refreshCookie.Value
				}
				if _, _, err := a.oidc.authenticate(r.Context(), idToken, refreshToken); err == nil {
					http.Redirect(w, r, a.baseURL, http.StatusFound)
					return
				}
			}
		}

		oidcLoginURL := ""
		if a.oidc != nil {
			oidcLoginURL = a.oidc.loginURL
		}
		components{endpoints: endpoints{}}.LoginPage("", oidcLoginURL).Render(w)
	})

	m.HandleFunc(http.MethodPost+" "+a.loginEndpoint, func(w http.ResponseWriter, r *http.Request) {
		oidcLoginURL := ""
		if a.oidc != nil {
			oidcLoginURL = a.oidc.loginURL
		}

		if err := r.ParseForm(); err != nil {
			components{endpoints: endpoints{}}.LoginPage("Invalid form submission", oidcLoginURL).Render(w)
			return
		}

		email := strings.TrimSpace(r.Form.Get("email"))
		password := r.Form.Get("password")

		user, err := a.repo.GetUser(r.Context(), email)
		if err != nil || user.PasswordHash == nil {
			components{endpoints: endpoints{}}.LoginPage("Invalid email or password", oidcLoginURL).Render(w)
			return
		}

		if err := bcryptCompare([]byte(password), []byte(*user.PasswordHash)); err != nil {
			components{endpoints: endpoints{}}.LoginPage("Invalid email or password", oidcLoginURL).Render(w)
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

		http.Redirect(w, r, a.baseURL, http.StatusFound)
	})

	// Self-login logout (clears session cookie)
	m.HandleFunc(http.MethodGet+" /logout", func(w http.ResponseWriter, r *http.Request) {
		clearSessionCookie(w)
		if a.oidc != nil {
			a.oidc.clearAllCookies(w)
		}
		http.Redirect(w, r, a.loginEndpoint, http.StatusFound)
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
