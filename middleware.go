package projektovemeeting

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

func MiddlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lr := &wrappedResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lr, r)

		slog.Info("",
			"Method", r.Method,
			"Path", r.RequestURI,
			"Status", strconv.Itoa(lr.statusCode),
			"Duration", time.Since(start).Microseconds(),
		)
	})
}

type wrappedResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (lr *wrappedResponseWriter) WriteHeader(code int) {
	if lr.wroteHeader {
		return
	}
	lr.statusCode = code
	lr.wroteHeader = true
	lr.ResponseWriter.WriteHeader(code)
}

func (lr *wrappedResponseWriter) Write(b []byte) (int, error) {
	if !lr.wroteHeader {
		lr.WriteHeader(http.StatusOK)
	}
	return lr.ResponseWriter.Write(b)
}
