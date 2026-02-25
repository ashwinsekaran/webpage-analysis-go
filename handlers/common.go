package handlers

import (
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

const (
	HeaderContentType = "Content-Type"
	MimeTextPlain     = "text/plain"
)

func Ok(path string) (string, string, httprouter.Handle) {
	return http.MethodGet, path, func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.Header().Set(HeaderContentType, MimeTextPlain)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Ok\n")
	}
}
