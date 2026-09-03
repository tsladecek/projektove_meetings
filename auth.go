package projektovemeeting

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
)

type AuthOIDC struct {
	repo     Repository
	verifier *oidc.IDTokenVerifier
}

func NewAuth(repo Repository, issuer string, clientID string) (Auth, error) {
	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return AuthOIDC{}, fmt.Errorf("when discovering oidc provider %q: %w", issuer, err)
	}

	return AuthOIDC{
		repo:     repo,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

type oidcClaims struct {
	Email string `json:"email"`
}

func (a AuthOIDC) Authenticate(ctx context.Context, token string) (User, error) {
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

type ctxKey string

const ctxKeyUser ctxKey = "user"

func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(User)
	return u, ok
}

func MiddlewareAuth(next http.Handler, auth Auth, cookieName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		user, err := auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			slog.Debug("Authentication failed", "error", err.Error())
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}
