# GMNG Detect

A tiny, open-source hardware detector for [GMNG](https://gmng.co.uk). It reads your PC's
components once and hands them to gmng.co.uk so the site can answer "will this game run?"
and "what should I upgrade?" for **your** actual machine.

## What it does

- Reads: system model, CPU, GPU(s) + driver, RAM (total, per-module, free slots),
  motherboard, BIOS, display resolution/refresh, storage, and OS build.
- Uploads that once to gmng.co.uk over HTTPS, keyed by a one-time random token, then opens
  your browser to finish.

## What it does NOT do

- No installer, no background service, no listening socket, no persistence.
- No personal data. It reads hardware inventory only.
- No telemetry beyond the single upload you triggered.

## Why it's open source

So you can read exactly what it does before you run it. Building trust is the point.

## Build

```
CGO_ENABLED=0 go build -ldflags="-s -w" .
```

Cross-compile: set `GOOS`/`GOARCH` (windows/amd64, darwin/arm64, darwin/amd64, linux/amd64).
No third-party dependencies — standard library only. Per-OS collection uses native tools
(WMI on Windows, `system_profiler` on macOS, `/sys` + `lspci` + `lsblk` on Linux).

## Privacy

The uploaded payload is your hardware inventory. It is held on gmng.co.uk for 15 minutes
until your browser claims it, then deleted. See https://gmng.co.uk/privacy.

## Licence

MIT © Westcountry Hosting Ltd
