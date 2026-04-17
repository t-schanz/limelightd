# limelightd

A tiny Linux daemon that turns your Elgato Key Lights on when your webcam opens and off when it closes. Event-driven, zero polling, ~3 MB RAM idle.

## Status

In development. See [`docs/superpowers/specs/2026-04-17-limelightd-design.md`](docs/superpowers/specs/2026-04-17-limelightd-design.md) for the design.

## Build

    go build -o limelightd ./cmd/limelightd

## Install (after build)

    install -Dm755 limelightd ~/.local/bin/limelightd
    install -Dm644 systemd/limelightd.service ~/.config/systemd/user/limelightd.service
    systemctl --user daemon-reload
    systemctl --user enable --now limelightd
