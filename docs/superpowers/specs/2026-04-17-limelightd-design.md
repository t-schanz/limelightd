# limelightd — Design Spec

**Date:** 2026-04-17
**Status:** Approved (pending spec review)

## Summary

`limelightd` is a single-binary Go daemon for Linux that detects when any local webcam (`/dev/video*`) is opened or closed and automatically toggles every Elgato Key Light on the local network to a configured preset. It is event-driven (no polling), runs as a systemd user service, and has no external daemon dependencies (no Home Assistant, no broker, no extra services).

## Goals

- Camera opens → all discovered Key Lights turn on with the user's preferred brightness/temperature.
- Camera closes → all discovered Key Lights turn off.
- Minimal resource footprint: single static binary, idle ≈3 MB RAM, 0% idle CPU.
- Zero-config for the common case: mDNS discovery handles finding the lights.
- Runs as the user (no root), survives light IP changes, survives daemon restarts.

## Non-Goals

- Controlling non-Elgato lights.
- Running on macOS or Windows (Linux-only; uses udev).
- Exposing a control API, CLI, or GUI. Control happens via the camera only.
- Per-light configuration. All discovered lights receive the same preset.
- State save/restore of the light's pre-activation settings.

## Architecture

Three loosely-coupled components inside a single process, wired together in `main`:

```
udev netlink  →  Camera Watcher  →  raw events  →  Debouncer  →  intents  →  Light Controller  →  HTTP PUT :9123/elgato/lights
                                                                                  ↑
                                                                           mDNS cache (_elg._tcp.local.)
```

### 1. Camera Watcher (`internal/camera`)

- Subscribes to the udev netlink socket via a Go udev library (e.g. `github.com/jochenvg/go-udev` or similar — final choice made during implementation).
- Filters for `subsystem == video4linux` and `ACTION == change`.
- The uvcvideo kernel driver emits a `change` uevent when the device is opened and when it is closed, but the uevent itself does not carry direction. The watcher resolves direction by tracking which processes currently hold a file descriptor on the device: on each event, scan `/proc/*/fd/*` for symlinks pointing at `/dev/videoN`. Open = at least one fd held; Close = none. The scan cost is O(processes) and runs only on events, not continuously.
- The `/proc` scan approach is the primary mechanism; it works without elevated privileges because we only need to read the calling user's own fds plus the basic symlink targets. If another user holds the camera fd, we fall back to assuming any `change` event transitions state. This edge case (multi-user desktops) is documented as a known limitation.
- Emits `CameraEvent{Kind: Open|Close, At: time.Time}` on a channel.
- Performs a startup scan of `/proc/*/fd/*` to detect cameras already in use when the daemon starts — if any are in use, emits an initial `Open`.

### 2. Debouncer (`internal/debounce`)

- Consumes raw `CameraEvent`s.
- Holds a pending state change for `on_ms` (default 200 ms) on Open and `off_ms` (default 200 ms) on Close.
- If a counter-event arrives within the debounce window, the pending change is cancelled.
- Emits stable `LightIntent{On: bool}` values only after the debounce window elapses without contradiction.
- Pure logic, channel-in/channel-out — easy to unit test with a table-driven approach.

### 3. Light Controller (`internal/light`)

- **Discovery** (`discovery.go`): uses mDNS (`_elg._tcp.local.`) to find Elgato lights on the network. Caches results in memory. Refreshes every `discovery.refresh_interval` (default 5 min) and on any HTTP failure (which invalidates the cache).
- **Controller** (`controller.go`): consumes `LightIntent`s. For each discovered light, fires a concurrent HTTP PUT to `http://<ip>:9123/elgato/lights` with the appropriate payload:
  - On `LightOn`: `{"numberOfLights":1,"lights":[{"on":1,"brightness":<cfg>,"temperature":<cfg>}]}`
  - On `LightOff`: `{"numberOfLights":1,"lights":[{"on":0}]}`
- Uses a short HTTP timeout (2 s) so a dead light can't block the next event.
- Logs warnings on failure but does not crash or retry in-band; the next camera event retries naturally.

### Main (`cmd/limelightd/main.go`)

- Loads config, constructs the three components, wires channels.
- Traps `SIGTERM` and `SIGINT`: on signal, sends a synchronous `LightOff` to all lights, then exits. This leaves the lights in a clean state when `systemctl --user stop limelightd` is issued.

## Configuration

File: `~/.config/limelightd/config.toml`. All fields optional; daemon starts with baked-in defaults if the file is absent.

```toml
[light]
brightness  = 20      # 3-100
temperature = 213     # 143 (cool, 7000K) - 344 (warm, 2900K)

[debounce]
on_ms  = 200
off_ms = 1000

[discovery]
refresh_interval = "5m"
```

Defaults embedded in the binary match the values shown above.

Logging level is controlled via the `LIMELIGHTD_LOG` environment variable (`info` default, `debug` for verbose).

## Deployment

- **Binary location:** `~/.local/bin/limelightd` (user-local, no sudo required).
- **Systemd unit:** `~/.config/systemd/user/limelightd.service` — auto-restart on failure, logs to journald via stderr.
- **Enable:** `systemctl --user enable --now limelightd`.
- **Permissions:** the udev netlink socket is open to unprivileged users, and `/proc/*/fd/*` is readable for the user's own processes. The daemon does not need to open `/dev/video*` itself. No root, no capabilities required. (If a future Linux hardening change restricts udev netlink to root, the fallback is running as a system service — called out here so that decision isn't silently re-litigated later.)

Sample unit file (ships in `systemd/limelightd.service`):

```ini
[Unit]
Description=Elgato Key Light camera sync daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/limelightd
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
```

## Error Handling

| Scenario | Behavior |
| --- | --- |
| Light HTTP PUT fails | Log warning, invalidate mDNS cache, continue. Next event retries. |
| mDNS finds no lights | Log once, keep running. Re-discovery fires every `refresh_interval`. |
| Camera already open at startup | Detect via `/proc/*/fd/*` scan, emit initial Open intent. |
| Config file missing | Use defaults silently. |
| Config file malformed | Log error, exit with non-zero status (systemd will restart; misconfigured daemon should be loud). |
| `SIGTERM` / `SIGINT` | Send Off to all lights synchronously, then exit. |
| udev subscription fails | Log error, exit with non-zero (systemd restart). This is unrecoverable in-process. |

## Logging

Structured plain-text lines to stderr, picked up by journald:

- One line per camera event received (debug level for raw events, info for debounced intents).
- One line per light action attempted, with outcome.
- Warnings for network failures.
- Errors for unrecoverable conditions.

No log files; no log rotation. `journalctl --user -u limelightd` is the user interface.

## Testing

- **Debouncer:** pure logic, table-driven unit tests with a synthetic clock.
- **Light Controller:** `httptest.Server` mocking the Elgato HTTP API; assert request payloads and behavior on failure.
- **Camera Watcher:** injectable interface around the udev reader; unit tests exercise event dispatch with a fake source.
- **Integration:** a `cmd/limelightd` boot test that wires real components against a fake udev source and a fake HTTP server, verifying end-to-end dispatch.
- **No CI hardware tests.** A `README.md` smoke-test checklist covers manual verification: open camera in browser → light on within ≤300 ms; close → light off within ≤300 ms; toggle twice in <200 ms → no flicker.

## Project Layout

```
limelightd/
├── cmd/limelightd/main.go        # wiring, signals, config load
├── internal/camera/watcher.go    # udev subscription + startup scan
├── internal/debounce/debounce.go
├── internal/light/discovery.go   # mDNS
├── internal/light/controller.go  # HTTP client + preset
├── internal/config/config.go     # TOML loader + defaults
├── systemd/limelightd.service
├── go.mod
└── README.md
```

## Open Questions

None at spec-approval time. Library choices (specific udev and mDNS packages) are deferred to the implementation plan.
