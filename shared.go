package projektovemeeting

import (
	"log/slog"
	"net/http"
)

func WriteError(w http.ResponseWriter, message string, status int, err error) {
	if err != nil {
		slog.Error(err.Error())
	}
	http.Error(w, message, status)
}
