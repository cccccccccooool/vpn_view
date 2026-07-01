# VPNView Adapter Interface

VPNView keeps the main program fixed. A protocol implementation only needs to
implement `port.VPNAdapter`, declare capabilities, and be selected in
`cmd/vpnview/main.go` or by `adapter.type` when compiled into the binary.

## Capabilities

Adapters return a `domain.Capability` bitmask from `Capabilities()`.

| Capability | Meaning |
| --- | --- |
| `CapListUsers` | List effective users in the core. |
| `CapAddUser` | Add a user to the core. |
| `CapRemoveUser` | Remove a user from the core. |
| `CapDisableUser` / `CapEnableUser` | Disable or re-enable users without deleting store records. |
| `CapQueryTraffic` | Return per-user cumulative traffic counters. |
| `CapRealtimeSpeed` | Return global realtime speed. |
| `CapUserSpeed` | UI may show per-user speed values. |
| `CapActiveConns` / `CapKillConn` | List or close active connections. |
| `CapSubscription` | Generate subscription content. |
| `CapCredentialDefs` | Provide dynamic credential fields for the create form. |
| `CapSpeedLimit` / `CapGlobalSpeedLimit` | Apply native speed limits. |

If a capability is not declared, return `domain.ErrNotSupported` from the
method and let the main program degrade gracefully.

Adapters may also implement `port.ProfileProvider` to expose a structured
`domain.AdapterProfile`. The profile does not replace the bitmask; it explains
how the adapter provisions users, reports traffic, resolves identity, applies
reloads, and stores configuration. New shared orchestration should prefer the
profile when it is available.

## Identity and Credentials

`domain.User.ID` is a generic primary key. It can be a UUID, username, email,
or any adapter-defined string.

`domain.User.Credentials` is a `map[string]string`. The main program stores
and passes this map through without interpreting it. Use `CredentialFields()`
to describe the fields the frontend should render.

Example:

```go
func (a *Adapter) CredentialFields() []port.CredentialField {
    return []port.CredentialField{
        {Key: "uuid", Label: "UUID", Type: "text", Required: true, AutoGenerate: true},
        {Key: "flow", Label: "Flow", Type: "select", Options: []string{"", "xtls-rprx-vision"}},
    }
}
```

## Traffic Semantics

`QueryTraffic(ctx)` must return cumulative counters for each user. The main
program calculates deltas between adjacent snapshots and persists those deltas.
Polling intervals below five seconds are discouraged for real cores.

## Concurrency

Handlers and background services can call the adapter concurrently. Adapter
implementations must protect mutable state with a mutex or use concurrency-safe
clients.

## Reference Implementations

`internal/adapter/stub` is a full mock implementation for local development.

`internal/adapter/singbox` shows a practical sing-box integration based on
configuration rewriting, reload commands, Clash API speed/connections, V2Ray
Stats gRPC traffic deltas, and subscription generation. The gRPC reader is
isolated behind `TrafficReader`, so protocol-specific stats parsing can be
swapped without touching the main program.
