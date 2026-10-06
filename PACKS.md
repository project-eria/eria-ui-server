# Widget packs

A **pack** is a family of widgets sharing one style: its own custom elements,
styles and fonts, rendering the ERIA capabilities (lights, shutters, sensors…).
Packs are added to a running ui-user **without recompiling**: drop a folder in
the server's packs directory. People choose a pack in *Preferences*, and a
custom screen can impose its own (screen > profile > **Standard**, the
built-in pack). Design: [PLAN.md §5.2](PLAN.md).

Looks (colors, fonts, spacing) are not a pack's job: they come from the
**themes** shipped with the UI (PLAN.md §5.3: Ardoise, Contraste — each light and
dark), through the CSS variables below. Test your pack in all four.

## Install

- Folder: `packs/<id>/` next to the binary (`packs:` in `config.yml`), or
  `{data}/packs/<id>/` when deployed by the ERIA orchestrator (`UI_USER_PACKS`).
- Read at every request: no restart. A pack that fails validation is listed in
  *Preferences* (admins) with the reason, and can't be chosen.
- **A pack is code run inside the app, with the rights of the signed-in
  person** (it can drive devices). Only install packs you trust; only people
  with access to the server can install one.

Example: [`packs-examples/contour/`](packs-examples/contour/) — lights only;
every other capability falls back to Standard.

## `pack.json`

```json
{
	"id": "contour",
	"name": "Contour",
	"version": "0.1.0",
	"sdk": 1,
	"entry": "index.js",
	"report": "REPORT.md",
	"widgets": {
		"LightBasic": { "tag": "contour-light", "sizes": { "tile": [[2, 1]], "value": [[2, 1]] } },
		"LightDimmer": { "tag": "contour-light" }
	}
}
```

| Key | Rule |
|---|---|
| `id` | = folder name; `a-z`, digits, `-`; not `standard` |
| `sdk` | SDK contract version the pack was written for (`1`); any other value is refused |
| `entry` | ES module defining the elements (default `index.js`) |
| `report` | optional accessibility report (A11Y.md template). Without it the pack is shown as **unverified** |
| `widgets` | capability → `tag` (**must start with `<id>-`**) and optional `sizes` per variant (`tile`, `full`, `value`; `[w, h]`, w 1–4, h 1–8; first = default). No `sizes`: Standard's |

Capabilities: `LightBasic`, `LightDimmer`, `ShutterBasic`, `ShutterPosition`,
`GarageDoor`, `Trigger`, `TemperatureSensor`, `VoltageSensor`,
`HygrometerSensor`, `BarometerSensor`, `WindgaugeSensor`, `WaterMeter`,
`UVSensor`, `PHSensor`, `ORPSensor`, `Contexts` (modes).

## SDK — `/sdk/eria-widgets.js`

Import everything from this stable URL (never bundle your own Lit):

```js
import { EriaWidget, css, html, formatValue, messages as m } from '/sdk/eria-widgets.js';
```

| Export | Use |
|---|---|
| everything from `lit` | `html`, `css`, `nothing`, `LitElement`… |
| `EriaWidget` | base class: follows the Thing's properties, sends commands, confirms, reports errors |
| `EriaLight`, `EriaShutter`, `EriaGarage`, `EriaTrigger`, `EriaSensor`, `EriaModes` | Standard widgets, to extend when changing only part of one |
| `widgetStyles` | Standard's shared styles |
| `formatValue`, `formatParts`, `propertyName`, `compass` | values, units, reading names, wind direction — localized |
| `messages`, `getLocale` | the app's localized texts (`light_on`, `light_off`, `light_on_at`, `no_value`, `open_action`…) |
| `sdkVersion` | `1` |

### Widget contract

Properties set by the app on your element (all lowercase — Svelte lowercases them):

| Property | Meaning |
|---|---|
| `thing` | handle: `read`, `write`, `invoke`, `observe` (use through the base class) |
| `schemas` | the service's properties (type, unit, min/max…) |
| `label` | name to show (the card's own name if set) |
| `variant` | `tile` · `full` · `value` |
| `confirm` | ask before acting (always on for the garage door by default) |
| `viewonly` | guest profile: no command; keep states fully legible |
| `online` | the Thing answers |
| `props` | readings to show (multi-value sensors) |
| `modes` | modes list (`Contexts`) |

From `EriaWidget`: override `watched()` to list the properties to follow
(values arrive in `this.values`); send commands with `this.act(action, input)`
— it handles confirmation and errors (`eria-error` event); `this.locked` is
true when commands are not allowed (offline or guest). **Commands are actions,
never property writes** (writing a KNX property does not drive the device).

### Looks: CSS variables only

Use the `--eria-*` variables (they cross the shadow DOM), so every theme
applies to your pack. Give a system-color fallback, so the widget stays
legible anywhere:

| Variable | Role |
|---|---|
| `--eria-bg`, `--eria-surface` | page and card backgrounds |
| `--eria-ink`, `--eria-muted` | text, secondary text |
| `--eria-line` | borders (3:1 on backgrounds) |
| `--eria-lit`, `--eria-lit-ink` | "switched on" — **amber means only that** — and its text |
| `--eria-focus`, `--eria-focus-width` | focus ring |
| `--eria-danger` | errors |
| `--eria-radius`, `--eria-pad`, `--eria-gap` | shapes and spacing |
| `--eria-glow` | the "lit" halo |
| `--eria-font` | inherited from the page, nothing to set |

### Accessibility (A11Y.md, WCAG 2.2 AA)

The app cannot check your widgets: ship a `REPORT.md` or the pack is shown as
**unverified**. At least: native controls (`button`, `input`), APG switch
(`role="switch"` + `aria-checked`) for on/off, the accessible name contains the
visible label, visible focus, targets ≥ 44 px, no information by color only,
`forced-colors` and `prefers-reduced-motion` respected, names never truncated,
no announcement of values pushed by the house. See
[A11Y-DECISIONS.md](A11Y-DECISIONS.md) for the patterns of the Standard pack.
