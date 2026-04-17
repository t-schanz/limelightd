# limelightd

A tiny Linux daemon that turns your Elgato Key Lights on when your webcam opens and off when it closes.

- Event-driven (udev netlink — no polling)
- Zero-config: finds lights via mDNS
- Single static binary, ≈3 MB RAM idle
- Systemd user service, no root required

See [`docs/superpowers/specs/2026-04-17-limelightd-design.md`](docs/superpowers/specs/2026-04-17-limelightd-design.md) for the full design.

## Build

```
go build -o limelightd ./cmd/limelightd
```

## Install

```
install -Dm755 limelightd ~/.local/bin/limelightd
install -Dm644 systemd/limelightd.service ~/.config/systemd/user/limelightd.service
systemctl --user daemon-reload
systemctl --user enable --now limelightd
```

## Configure (optional)

Create `~/.config/limelightd/config.toml` to override defaults:

```toml
[light]
brightness  = 20      # 3-100
temperature = 213     # 143 (cool) - 344 (warm)

[debounce]
on_ms  = 200
off_ms = 1000

[discovery]
refresh_interval = "5m"
```

## Logs

```
journalctl --user -u limelightd -f
```

Set `LIMELIGHTD_LOG=debug` in the service unit's `[Service]` section (via `Environment=`) for verbose logs.

## Manual smoke test

After install:

1. `journalctl --user -u limelightd -f` in one terminal.
2. Open your camera in a browser (e.g. a meet site) — the key light should turn on within ≈300 ms.
3. Close the camera tab — the light should turn off within ≈1300 ms (200 ms event latency + 1 s off-debounce).
4. Quickly open and close (<1 s) — the light should not visibly flicker.

## Troubleshooting

- **Light never turns on**: confirm discovery with `journalctl --user -u limelightd | grep discovery`. If it logs `lights=0`, verify your Key Light is reachable on the network (ping it and `curl http://<ip>:9123/elgato/lights`).
- **Light turns on but not off**: the daemon watches `/proc/*/fd/*` to detect close. If your camera is being held open by a different user's process, the close event won't be observed (documented limitation).
- **Permission errors on startup**: confirm the binary is at `~/.local/bin/limelightd` and that systemd-user has access to udev netlink (it should by default on modern distros).

## Development

```
go test ./... -race
```
