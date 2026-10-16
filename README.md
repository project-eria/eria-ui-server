# eria-ui-server

Relay between the ERIA app ([eria-ui-app](https://github.com/project-eria/eria-ui-app)) and
the services of the house (lights, shutters, contexts…): mDNS discovery of the WoT Things, one
WebSocket per app for commands, observations, events and settings (below), profiles + PIN,
terminals, pages. Design and plan: eria-ui-app's `PLAN.md` (§2.1, §2.2).

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
| `services` | — | services, each with `can {read, write, invoke}` for this connection |
| `contexts` | — | list |
| `page` | `value`: id | page (shared, or the profile's own) |
| `pages` | — | pages the profile may edit (all for level 0, else its own) |
| `savePage` | `value`: page (no `id` = new; same `version` as stored) | page |
| `deletePage` | `value`: id | `true` |
| `read` | `thing`, `name` | value |
| `write` | `thing`, `name`, `value` | `null` (signed in only) |
| `invoke` | `thing`, `name`, `value` (input) | output (signed in only) |
| `observe` / `unobserve` | `thing`, `name` (property) | `true` |
| `subscribe` / `unsubscribe` | `thing`, `name` (event) | `true` |
| `adminProfiles` | — | profiles with their role (level 0) |
| `createProfile` / `updateProfile` / `deleteProfile` | `value`: `{name, role, pin}` / `{id, name?, role?, pin?}` / id | profile / `true` (level 0) |
| `terminals`, `hidden` | — | lists (level 0) |
| `configureTerminal` | `value`: `{id, name?, type?, theme?, aspect?, pages?}` | terminal (level 0) |
| `forgetTerminal` | `value`: id | `true` (level 0) |
| `setContexts` | `value`: `[{id, label}]` | `true` (level 0) |
| `setHidden` | `value`: glob patterns | `true` (level 0) |

Pushed: `welcome {terminal, profile}` then `resources {theme, aspect, pages: [{id, version}]}`
first (again after sign-in or out, and whenever a page or the terminal changes), then
`value {thing, property, value}`,
`event {thing, event, value}`, `online {thing, online}`, `things-changed`, `contexts-changed`,
`terminal {terminal}` (its settings changed). Levels are provisional (admin 0, member 1,
guest 9 = not signed in) until accounts move to `auth`.

A page (eria-ui-app's PLAN §5.00) is `{id, title, shared, version, elements, options}`;
an element is `{id, type, …, label?, hidden?, widget?, options?}` with `type` `service`
(`thing`, `capability`), `property` / `action` / `event` (`thing`, `name`, action `input?`) or
`app` (`name`). Shared pages are made by level 0 and assigned to terminals; personal pages
follow their profile. `can` is provisional (reading for all, commands below level 9) until
`authz` computes it.

Besides the channel, HTTP only serves the health probe (`/api/health`).

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
