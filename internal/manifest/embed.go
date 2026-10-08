package manifest

import (
	"embed"
	"encoding/json"
)

//go:embed manifest.json
var manifestFS embed.FS

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
