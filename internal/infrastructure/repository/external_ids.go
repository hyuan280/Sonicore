package repository

import (
	"encoding/json"

	"github.com/sonicore/server/pkg/utils"
)

// marshalExternalIDs encodes an external-id alias map for the JSONB columns
// (artists.external_ids / albums.external_ids). A nil/empty map becomes {}.
func marshalExternalIDs(m map[string]string) ([]byte, error) {
	if m == nil {
		m = map[string]string{}
	}
	return json.Marshal(m)
}

// sourceOrDefault maps an empty metadata source to the default source.
func sourceOrDefault(s string) string {
	if s == "" {
		return utils.DefaultSource
	}
	return s
}
