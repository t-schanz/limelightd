package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Defaults()

	if c.Light.Brightness != 20 {
		t.Errorf("Brightness = %d, want 20", c.Light.Brightness)
	}
	if c.Light.Temperature != 213 {
		t.Errorf("Temperature = %d, want 213", c.Light.Temperature)
	}
	if c.Debounce.OnMs != 200 {
		t.Errorf("OnMs = %d, want 200", c.Debounce.OnMs)
	}
	if c.Debounce.OffMs != 1000 {
		t.Errorf("OffMs = %d, want 1000", c.Debounce.OffMs)
	}
	if c.Discovery.RefreshInterval != 5*time.Minute {
		t.Errorf("RefreshInterval = %v, want 5m", c.Discovery.RefreshInterval)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	c, err := Load("/nonexistent/path.toml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Light.Brightness != 20 {
		t.Errorf("expected defaults, got Brightness=%d", c.Light.Brightness)
	}
}

func TestLoad_ParsesOverrides(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	body := `
[light]
brightness = 75
temperature = 180

[debounce]
on_ms = 50
off_ms = 500

[discovery]
refresh_interval = "2m"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Light.Brightness != 75 {
		t.Errorf("Brightness = %d, want 75", c.Light.Brightness)
	}
	if c.Light.Temperature != 180 {
		t.Errorf("Temperature = %d, want 180", c.Light.Temperature)
	}
	if c.Debounce.OnMs != 50 {
		t.Errorf("OnMs = %d, want 50", c.Debounce.OnMs)
	}
	if c.Debounce.OffMs != 500 {
		t.Errorf("OffMs = %d, want 500", c.Debounce.OffMs)
	}
	if c.Discovery.RefreshInterval != 2*time.Minute {
		t.Errorf("RefreshInterval = %v, want 2m", c.Discovery.RefreshInterval)
	}
}

func TestLoad_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("this is not = valid = toml ="), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(p); err == nil {
		t.Fatal("expected error on malformed TOML, got nil")
	}
}
