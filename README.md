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
| `profiles` | — | `[{id, name}]` (sign-in screen) |
| `login` | `profile`, `pin` | `{token, profile}`; the connection is signed in |
| `logout` | — | `true` |
| `reportType` | `value`: `watch` … `tv` | `true` (type deduced from the screen) |
| `services` | — | services, each with `can {read, write, invoke}` for this connection |
| `contexts` | — | list |
| `page` | `value`: id | page |
| `pages` | — | every page |
| `savePage` | `value`: page (no `id` = new, with a known `theme` — `""` = Base; same `version` as stored; `theme`, `aspect`, `layout` fixed after creation) | page (level 0) |
| `deletePage` | `value`: id | `true` (level 0) |
| `read` | `thing`, `name` | value (hidden Things: level 0 only, as for every Thing op) |
| `write` | `thing`, `name`, `value` | `null` (signed in only) |
| `invoke` | `thing`, `name`, `value` (input) | output (signed in only) |
| `notices` | — | notices of service-notices (`notices:` in config) this connection may see, newest first, each with `read` (`audience: profile` ones for a signed-in person only; a resolved one leaves the list 24 h after its resolution once read — read or not without a person); `[]` without the service |
| `markRead` | `value`: notice ids | `true` (signed in only) |
| `observe` / `unobserve` | `thing`, `name` (property) | `true` |
| `subscribe` / `unsubscribe` | `thing`, `name` (event) | `true` |
| `adminProfiles` | — | profiles `{id, name, level, lastLogin?}` (level 0) |
| `createProfile` / `updateProfile` / `deleteProfile` | `value`: `{name, level, pin}` / `{id, name?, level?, pin?}` / id | profile / `true` (level 0; the last level-0 profile can be neither deleted nor demoted) |
| `things` | — | every known Thing `{id, title, url, online, hidden, manual}` (level 0) |
| `td` | `thing` | the Thing's TD as received (level 0) |
| `addThing` | `value`: TD URL or host (then `/.well-known/wot`) | `{id, url}`; kept and polled (level 0) |
| `forgetThing` | `thing` | `true`, for a Thing added by address (level 0) |
| `themes` | — | signed themes of the themes folder `{id, name, version, hash, aspects, layouts, report, error?}`, for creating a page (level 0) |
| `theme` | `value`: id | `{archive (base64), signature}` |
| `terminals`, `hidden` | — | lists (level 0); a terminal is `{id, code, name, type, detectedType, lastSeen, homePage, online}` |
| `configureTerminal` | `value`: `{id, name?, type?, homePage?}` (`homePage`: a page id, `""` = the house's) | terminal (level 0) |
| `setHomePage` | `value`: page id, `""` = none | `true`; the house's start page, for terminals without their own (level 0) |
| `forgetTerminal` | `value`: id | `true` (level 0) |
| `setContexts` | `value`: `[{id, label}]` | `true` (level 0) |
| `setHidden` | `value`: glob patterns | `true` (level 0) |

Pushed: `welcome {terminal, profile}` then `resources {pages: [{id, version}], themes: [{id, hash}], homePage}`
(every page, and the signed themes they use) first (again after sign-in or out, and whenever a
page changes), then
`value {thing, property, value}`,
`event {thing, event, value}`, `online {thing, online}`, `things-changed`, `contexts-changed`,
`terminal {terminal}` (its settings changed), `notices-changed` (ask `notices` again). Each profile has a stored level 0-9 (0 =
administration, 5 = member, 9 = read only, as when not signed in); older role databases are
migrated (admin → 0, member → 5, guest → 9).

A page (eria-ui-app's PLAN §5.000) is `{id, title, theme, aspect, layout, version, elements, options}`;
an element is `{id, type, …, label?, hidden?, widget?, options?}` with `type` `service`
(`thing`, `capability`), `property` / `action` / `event` (`thing`, `name`, action `input?`) or
`app` (`name`). Every page is shown on every terminal; pages are made and changed by level 0 only. `can` is provisional (reading for all, commands below level 9) until
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
