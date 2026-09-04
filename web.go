package projektovemeeting

import (
	"fmt"
	"net/http"
	"path"
)

func pattern(method, base, endpoint string) string {
	return fmt.Sprintf("%s %s", method, path.Join(base, endpoint))
}

type handler struct {
	controller Controller
}

func NewHandler(auth Auth, basePath, cookieName string, controller Controller) http.Handler {
	m := http.NewServeMux()

	h := handler{controller: controller}

	m.Handle(pattern(http.MethodGet, basePath, "/"), auth.Middleware(h.root()))

	auth.RegisterRoutes(m)

	handler := MiddlewareLogging(m)
	return handler
}

func (h handler) root() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Root"))
	}
}
