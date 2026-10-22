# Wavelength

A self-hosted music server in Go. Early development; not yet a playable Subsonic replacement.

## Run

Requires Go 1.24 or newer. SQLite is embedded; no database server or CGO required.

```sh
go run ./cmd/wavelength
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

Configuration: `WAVELENGTH_ADDR` (default `127.0.0.1:8080`) and
`WAVELENGTH_DATA_DIR` (default `./data`). Schema migrations run on startup.
The server binds to loopback deliberately; authentication is not implemented yet.

## Development

```sh
make check
make build
```

See [architecture](docs/architecture.md) for boundaries and next milestones.
