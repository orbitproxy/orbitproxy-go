# orbitproxy-go

Go SDK for OrbitProxy. An **SDK client** is a virtual `machine`.

```text
orbitproxy (public)     → Register / Start / Connect / Identity
service/ (public)       → Service (reconnect, login, endpoints, Listen)
internal/gateway_ctl    → gateway control session (+ DiscoverTools)
internal/endpoint       → endpoint config/runtime + Forward/Listen
internal/mcpdiscover    → local MCP tools/list client
internal/controlplane   → POST /v1/machines/register (SDK restore)
wire/                   → shared protocol
```

## Install

```bash
go get github.com/orbitproxy/orbitproxy-go
```

## Usage

Register and Start are separate. **Only Start is shared** with the CLI runtime.
SDK Register stays restore-only (`AuthToken` + `MachineKey` required). CLI does its own create/register, then calls the same `Start`.

```go
id, err := orbitproxy.Register(ctx, orbitproxy.RegisterOptions{
    AuthToken: os.Getenv("ORBITPROXY_AUTHTOKEN"),
    MachineKey: os.Getenv("ORBITPROXY_MACHINE_KEY"), // required
    APIURL:    os.Getenv("ORBITPROXY_API_URL"),
})

svc, err := orbitproxy.Start(ctx, *id, orbitproxy.StartOptions{
    // Logger zero value → default dual-channel (stderr Info+, file Debug+).
    // Logger: LoggerOptions{Dir: "/data/op", MaxSizeMB: 200, ...}
    // Logger: LoggerOptions{Slog: DiscardLogger()} or ORBITPROXY_LOG_DISABLED=1
    // Optional lifecycle hooks: OnConnected, OnEndpoints, ...
})
defer svc.Close()
```

Convenience (SDK Register + Start):

```go
svc, err := orbitproxy.Connect(ctx, orbitproxy.Options{
    AuthToken: os.Getenv("ORBITPROXY_AUTHTOKEN"),
    MachineKey: os.Getenv("ORBITPROXY_MACHINE_KEY"),
    APIURL:    os.Getenv("ORBITPROXY_API_URL"),
})
```

**Forward** (console sets `localAddr`) — automatic after `Start`.

**Listen** (console sets `delivery: in_process`) — in-process `net.Listener`:

```go
ln, err := svc.Listen(ctx) // or service.WithEndpointID("ep_xxx")
http.Serve(ln, mux)
```

## API notes

- SDK `Register`: restore only; requires `AuthToken` + `MachineKey` + `APIURL`. Response needs `edge.addr`. Returns `Identity` for `Start` (MachineKey from input, EdgeAddr from CP, generated private key).
- `Start(Identity)`: shared runtime entry. No register call. CLI builds `Identity` from its own register/config (`machine_key` + keys + `edge_addr`).
- `Connect` = SDK `Register` + `Start`.
- `Version()` returns a semver: ldflags injection, then tagged module build info, then `ReleaseVersion` from the `VERSION` file (`go:embed`). It never returns `dev` / `devel`. Bump with `scripts/bump-version.sh 0.1.4`, then `git tag v0.1.4`. Register / ClientHello use this unless the caller passes `RegisterOptions.Version` / `StartOptions.SoftVersion`.
- Edge login uses ClientHello `machine_key`.
- Discover: `DiscoverTools` (`D`) / `DiscoverToolsResult` (`F`) with `request_id`. Create-sync attaches `NewEndpoint.discover_tools`; manual rediscover sends a standalone `DiscoverTools`.
- Discover may rewrite HTTP `Host` to `localhost:port` for Playwright local MCP; data-plane Host rewrite belongs on Edge (`host_rewrite`).
- Logging is nested under `Logger LoggerOptions`. Zero value enables defaults: stderr (Info+) + file (Debug+) at `~/.orbitproxy/<machineKey>/logs/orbitproxy.log` (lumberjack: 100MB / 7 days / 14 backups). Set `Logger.Dir` / `ORBITPROXY_LOG_DIR` and rotation fields / `ORBITPROXY_LOG_MAX_*` to override. `Logger.Slog` non-nil replaces defaults entirely; `DiscardLogger()` or `ORBITPROXY_LOG_DISABLED=1` silences defaults.
- Edge control conn is always TCP→TLS→yamux.
- `HTTPClient`: optional; used only for control-plane register. Never used for edge traffic.

## Development

```bash
go test ./...
```
