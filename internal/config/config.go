package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Light     LightConfig     `toml:"light"`
	Debounce  DebounceConfig  `toml:"debounce"`
	Discovery DiscoveryConfig `toml:"discovery"`
	Poll      PollConfig      `toml:"poll"`
}

type LightConfig struct {
	Brightness  int `toml:"brightness"`
	Temperature int `toml:"temperature"`
}

type DebounceConfig struct {
	OnMs  int `toml:"on_ms"`
	OffMs int `toml:"off_ms"`
}

type DiscoveryConfig struct {
	RefreshInterval time.Duration `toml:"refresh_interval"`
}

type PollConfig struct {
	Interval time.Duration `toml:"interval"`
}

func Defaults() Config {
	return Config{
		Light:     LightConfig{Brightness: 20, Temperature: 213},
		Debounce:  DebounceConfig{OnMs: 200, OffMs: 1000},
		Discovery: DiscoveryConfig{RefreshInterval: 5 * time.Minute},
		Poll:      PollConfig{Interval: 500 * time.Millisecond},
	}
}

// Load reads a TOML config file from path and merges it over defaults.
// A missing file is not an error — defaults are returned.
// A malformed file returns an error.
func Load(path string) (Config, error) {
	c := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	if _, err := toml.Decode(string(data), &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
