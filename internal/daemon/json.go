package daemon

import "encoding/json"

// jsonUnmarshal is a tiny wrapper to keep daemon.go free of stdlib
// JSON imports for readability. It returns nil on empty input.
func jsonUnmarshal(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}
