package rest

import (
	"encoding/json"
	"net/http"

	"github.com/sonicore/server/internal/core/domain"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeCodedError(w http.ResponseWriter, status int, code domain.ErrorCode) {
	writeJSON(w, status, map[string]interface{}{
		"error": code.DefaultMessage(),
		"code":  int(code),
	})
}
