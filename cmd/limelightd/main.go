package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"limelightd/internal/camera"
	"limelightd/internal/config"
	"limelightd/internal/debounce"
	"limelightd/internal/events"
	"limelightd/internal/light"
)

func main() {
	setupLogging()

	cfgPath := defaultConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "path", cfgPath,
		"brightness", cfg.Light.Brightness,
		"temperature", cfg.Light.Temperature,
		"on_ms", cfg.Debounce.OnMs,
		"off_ms", cfg.Debounce.OffMs,
	)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Components ---

	source, err := camera.NewUdevSource(rootCtx)
	if err != nil {
		slog.Error("udev setup failed", "err", err)
		os.Exit(1)
	}
	defer source.Close()

	probe := camera.NewProcProbe()
	watcher := camera.NewWatcher(source, probe)

	rawEvents := make(chan events.CameraEvent, 16)
	intents := make(chan events.LightIntent, 16)

	deb := debounce.New(
		debounce.Config{
			OnDelay:  time.Duration(cfg.Debounce.OnMs) * time.Millisecond,
			OffDelay: time.Duration(cfg.Debounce.OffMs) * time.Millisecond,
		},
		debounce.SystemClock(),
	)

	discoverySrc := light.NewCachedSource(light.DiscoverElgato, cfg.Discovery.RefreshInterval, 2*time.Second)

	controller := light.NewController(
		discoverySrc,
		light.Preset{Brightness: cfg.Light.Brightness, Temperature: cfg.Light.Temperature},
		2*time.Second,
	)

	// --- Wire goroutines ---

	go discoverySrc.Run(rootCtx)

	devices, err := camera.ListVideoDevices()
	if err != nil {
		slog.Warn("video device enumeration failed", "err", err)
	}
	go watcher.RunWithInitialScan(rootCtx, rawEvents, devices)

	go deb.Run(rootCtx, rawEvents, intents)
	go controller.Run(rootCtx, intents)

	<-rootCtx.Done()
	slog.Info("shutdown signal received")

	// Graceful: send Off synchronously, using a short detached context so
	// cancellation of rootCtx doesn't immediately kill the HTTP request.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutCancel()
	controller.ApplyOffBlocking(shutCtx)
	slog.Info("shutdown complete")
}

func defaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "limelightd", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "limelightd", "config.toml")
}

func setupLogging() {
	level := slog.LevelInfo
	if v := strings.ToLower(os.Getenv("LIMELIGHTD_LOG")); v == "debug" {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
