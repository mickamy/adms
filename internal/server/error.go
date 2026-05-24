package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/mickamy/adms/internal/logger"
)

const problemTypePrefix = "https://adms.dev/errors/"

// Problem is the RFC 7807 "Problem Details" response body.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// writeProblem responds with an RFC 7807 Problem Details body. The typeSuffix
// is appended to the adms problem URI prefix to form Problem.Type. If JSON
// encoding fails for any reason, writeProblem falls back to a plain-text 500
// and logs the encoding error.
func writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	typeSuffix, title, detail string,
) {
	body, err := json.Marshal(Problem{
		Type:     problemTypePrefix + typeSuffix,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.RequestURI(),
	})
	if err != nil {
		logger.Error(r.Context(), "encode problem details",
			"method", r.Method,
			"path", r.URL.EscapedPath(),
			"err", err.Error())
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "internal server error\n")

		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
