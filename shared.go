package projektovemeeting

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"
	"uuid"
)

func CurrentTime() time.Time {
	return time.Now().UTC()
}

func newUUID() string {
	return uuid.NewV4().String()
}

func WriteError(w http.ResponseWriter, message string, status int, err error) {
	if err != nil {
		slog.Error(err.Error())
	}
	http.Error(w, message, status)
}

type Endpoint struct {
	method  string
	path    string
	pathRaw string
}

func (e Endpoint) Pattern() string {
	return e.method + " " + e.path
}

// with base
func (e Endpoint) Path() string {
	return e.path
}

// without base
func (e Endpoint) PathRaw() string {
	return e.pathRaw
}

func NewEndpoint(method, base, path string) Endpoint {
	p, err := url.JoinPath(base, path)
	if err != nil {
		panic(err)
	}

	// since path can contain wildcards
	unescaped, err := url.PathUnescape(p)
	if err != nil {
		panic(err)
	}
	return Endpoint{method: method, path: unescaped, pathRaw: path}
}
