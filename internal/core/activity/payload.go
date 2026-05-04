package activity

import "encoding/json"

// unmarshalPayload is a thin wrapper so list.go can decode JSONB
// payloads without importing encoding/json directly — keeps the test
// surface and the production surface using the same single helper.
func unmarshalPayload(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
