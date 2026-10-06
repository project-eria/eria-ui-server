# A11Y Decisions Log (Pattern Memory)

> **Purpose:** cross-turn memory of choices between **equally conformant alternatives**. Two implementations can both pass `A11Y.md` and axe and still diverge (different `role`, focus pattern, announcement wording) — twenty compliant modals, zero coherence. This file prevents that.

## Rules for the AI

1. **Record only what is not derivable** from `A11Y.md` rules or from the code itself. Landmarks, headings, and alt presence are machine-verifiable — they do **NOT** belong here.
2. **Index by pattern, never by screen.** ✅ *"Destructive confirmation modal → `alertdialog`"* · ❌ *"The dispute modal does X"*.
3. **One line per decision:** pattern → choice → short why.
4. **Read before building:** before generating any interactive component, check this log and reuse the recorded pattern (see *Component Reuse* in the AI Behavior Contract).
5. **Never fork silently:** if a new requirement contradicts a recorded decision, ask the user — do not create a parallel variant.
6. **Stay lean:** tens of lines, not hundreds. This file shares the context budget with Lazy Loading; if it grows past ~40 entries, consolidate.
7. **Versioned, never gitignored:** this is *shared* memory — across turns, agents and developers. A local-only copy per developer forks the patterns and defeats the file's entire purpose.

## Decisions

<!-- Append entries below. Format: - **Pattern** → choice — why. (date) -->

- **On/off control (light, mode, generic boolean)** → `button role="switch"` + `aria-checked`, visible state text `aria-hidden` — the name never changes, the state is read once (APG switch). (2026-10-05)
- **Same command repeated per item (Open, Details, Move up…)** → visible verb first + visually hidden item name in the accessible name — unique names, label-in-name kept for voice control (SC 2.5.3). (2026-10-05)
- **Command acting on a physical device that can hurt (garage door)** → native `<dialog>` with `showModal()`, `role` dialog, initial focus on Cancel, Esc cancels; per-card `confirm` setting, on by default for `GarageDoor`. (2026-10-05)
- **Details / settings overlay** → native modal `<dialog>` named by its heading (`aria-labelledby`); focus return left to the browser; unsaved settings → confirmation before Esc/Cancel discards them. (2026-10-06)
- **Reordering cards or items** → Move up / Move down buttons, `aria-disabled` (not `disabled`) at the ends so focus stays, polite announcement "moved to position n of m"; no drag-and-drop (SC 2.5.7). (2026-10-06)
- **Edit mode of a page** → widgets `inert` while editing; on entry focus the edit-bar heading + polite announcement; on exit focus back on "Edit the page". Draft saved in one go. (2026-10-06)
- **Read-only profile (guest)** → controls `disabled` without dimming: states stay fully legible; the backend refuses the commands anyway. (2026-10-06)
- **Command failure** → persistent message in an always-present `role="alert"` container, with a named Dismiss button; never auto-dismissed. (2026-10-05)
- **Value changes pushed by the house (sensors, lamps switched elsewhere)** → not announced (no live region per widget) — avoids constant noise; the user reads values on demand. (2026-10-05)
- **SPA route change** → SvelteKit default: focus reset to the top of the document + its route announcer + unique `<title>` per screen. (2026-10-06)
- **Slider for a remote device (dimmer, shutter opening)** → native `input type=range`, command sent on `change` (release), `aria-valuetext` with the unit. (2026-10-05)
- **Card grid** → no `grid-auto-flow: dense` (visual order = reading order); rows grow with content; names never truncated (SC 1.4.10, 1.4.12). (2026-10-06)
- **Motion** → only the "lit" glow (box-shadow) transitions, under `prefers-reduced-motion: no-preference`; background and text color switch together so contrast never breaks mid-transition. (2026-10-05)
- **Profile + PIN sign-in** → profiles as a native radio group (`fieldset`/`legend`), PIN `type=password` + `inputmode=numeric` + `autocomplete=current-password`, paste allowed; error/lockout in `role="alert"` linked by `aria-describedby`, focus back to the PIN field. (2026-10-06)
- **Navigation on a phone** → bottom bar of text links scrolling horizontally on its own axis (no icons yet); on tablet/desktop a side rail that scrolls vertically when it outgrows the screen. (2026-10-06)
- **Widget pack choice** → native radio group; a pack without accessibility report is labelled "(unverified)" in its name, with help text pointing back to Standard; a pack that fails to load falls back to Standard with a status message. (2026-10-06)
- **Themes** → only `--eria-*` values per theme × scheme (`<html data-theme data-scheme>`); every shipped theme is measured in REPORT.md; Contraste offered for low vision (text ≥ 7:1, 4 px focus, no halo); light/dark is a separate Auto/Light/Dark choice. (2026-10-06)
