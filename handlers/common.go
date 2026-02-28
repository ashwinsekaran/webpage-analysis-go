package handlers

import (
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

const (
	// HeaderContentType is the canonical HTTP header key used across handlers.
	HeaderContentType = "Content-Type"
	// MimeTextPlain is used by lightweight health endpoints.
	MimeTextPlain     = "text/plain"
)

// Ok returns a simple GET handler that responds with "Ok" and HTTP 200.
func Ok(path string) (string, string, httprouter.Handle) {
	return http.MethodGet, path, func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.Header().Set(HeaderContentType, MimeTextPlain)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Ok\n")
	}
}
