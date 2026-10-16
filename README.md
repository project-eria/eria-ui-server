# eria-ui-server

Relay between the ERIA app ([eria-ui-app](https://github.com/project-eria/eria-ui-app)) and
the services of the house (lights, shutters, contexts…): mDNS discovery of the WoT Things, one
WebSocket per app for commands, observations, events and settings (below), profiles + PIN,
terminals, custom screens, widget packs. Design and plan: eria-ui-app's `PLAN.md` (§2.1, §2.2).

## Channel (`/api/ws`)

Handshake headers: `X-Eria-Terminal: <secret>` (generated and kept by the app, ≥ 128 bits,
required; an unknown terminal is registered) and, once signed in, `Authorization: Bearer <token>`.
The Rust client sends no `Origin`; a cross-origin browser page is refused.

The app sends `{"id", "op", …}`; each request gets one reply `{"id", "result"}` or
`{"id", "error": {"type", "message"}}`, with `type` one of `offline`, `refused`, `timeout`,
`invalid`, `failed` (`retryAfter` seconds for a locked profile). Replies may arrive in any order.

| Op | Fields | Result |
|---|---|---|
| `profiles` | — | `[{id, name, level}]` |
| `login` | `profile`, `pin` | `{token, profile}`; the connection is signed in |
| `logout` | — | `true` |
| `reportType` | `value`: `watch` … `tv` | `true` (type deduced from the screen) |
| `services`, `contexts` | — | lists |
| `read` | `thing`, `name` | value |
| `write` | `thing`, `name`, `value` | `null` (signed in only) |
| `invoke` | `thing`, `name`, `value` (input) | output (signed in only) |
| `observe` / `unobserve` | `thing`, `name` (property) | `true` |
| `subscribe` / `unsubscribe` | `thing`, `name` (event) | `true` |
| `terminals`, `hidden` | — | lists (level 0) |
| `configureTerminal` | `value`: `{id, name?, type?, theme?, aspect?, pages?}` | terminal (level 0) |
| `forgetTerminal` | `value`: id | `true` (level 0) |
| `setContexts` | `value`: `[{id, label}]` | `true` (level 0) |
| `setHidden` | `value`: glob patterns | `true` (level 0) |

Pushed: `welcome {terminal, profile}` first, then `value {thing, property, value}`,
`event {thing, event, value}`, `online {thing, online}`, `things-changed`, `contexts-changed`,
`terminal {terminal}` (its settings changed). Levels are provisional (admin 0, member 1,
guest 9 = not signed in) until accounts move to `auth`.

Profiles, screens and preferences still use HTTP (`/api/profiles`, `/api/screens`, `/api/me`)
with the same bearer token, until lots 15 and 17.

## Develop

```sh
GOWORK=off go test ./...
```

Local server: `config.yml` (from `config.sample.yml`) with `database: dev.db`, and test PINs
in `dev.env` (git-ignored): `set -a; . ./dev.env; set +a; go run . --config-path config.yml`.

## Build

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build -o eria-ui-server .
ERIA_UI_SERVER_ADMIN_PIN=<4-8 digits> ./eria-ui-server --config-path config.yml   # first start creates profile 'Admin'
```

The module uses the published `eria-core` / `go-wot`: build with `GOWORK=off`.

## Release and deployment (ERIA orchestrator)

Repository `project-eria/eria-ui-server`. Pushing a `vX.Y.Z` tag runs the shared ERIA release
workflow (`project-eria/workflows`): GoReleaser publishes the raw binaries, `eria-app.yml`
and `checksums.txt`, listed by the master's catalog. Deploy it from the Catalogue:

- **pin it to one node**: profiles, sessions and screens live in the agent's data folder of
  that node (`{data}/eria-ui-server.db`, kept across reinstalls);
- in the config, set `adminPin` (first start only, then change the PIN in *Profiles*).
