package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"llmctl/internal/paths"
)

// FileConfig holds optional `llmctl up` defaults read from
// $LLMCTL_HOME/config.json (F816). Explicit flags always win over the file.
type FileConfig struct {
	Addr        string   `json:"addr,omitempty"`         // listen address, e.g. ":8080"
	RateRPS     *float64 `json:"rate_rps,omitempty"`     // sustained req/s per client
	RateBurst   *int     `json:"rate_burst,omitempty"`   // burst size per client
	MaxInflight *int     `json:"max_inflight,omitempty"` // max concurrent gateway requests
	APIKey      string   `json:"api_key,omitempty"`      // optional static gateway key
}

// ConfigPath is the config file location.
func ConfigPath() string { return filepath.Join(paths.Home(), "config.json") }

// LoadFileConfig reads the config file. A missing file is not an error:
// the zero config is returned with found=false.
func LoadFileConfig() (cfg FileConfig, found bool, err error) {
	b, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, false, nil
		}
		return FileConfig{}, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return FileConfig{}, true, fmt.Errorf("bad %s: %w", ConfigPath(), err)
	}
	return cfg, true, nil
}
