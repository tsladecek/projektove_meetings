package projektovemeeting

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
)

func WriteError(w http.ResponseWriter, message string, status int, err error) {
	if err != nil {
		for {
			slog.Error(err.Error())
			err := errors.Unwrap(err)
			if err == nil {
				break
			}
		}
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
