# A11y Verification Report — Pastille widget pack

This report compiles the compliance evidence for a given *Feature*, ensuring its development reached the "Certification Ready" definition.

> This report is a **versioned project record** — never add it to `.gitignore`. Evidence hidden from version control is not evidence: QA and leadership verify it before release, and audits read it after.

> **Marking legend (mandatory):**
> `[x]` verified, with the evidence described beside it · `[!]` verified and **failed** (fix it, or open an entry in `EXCEPTIONS.md`) · `[~]` partially verified, with what is missing written down · `[ ]` **not verified** — the reason MUST be written beside it.
> Marking `[x]` without reproducible evidence invalidates the whole report.

> **Headless agents.** An AI without a browser MUST still produce this report, marking `[ ]` every checkpoint that requires a browser or assistive technology, with the reason and who must run it. A partial, honest report is evidence; a missing report is not. Overall status is CONDITIONAL — never PASS — while any `[ ]` or `[!]` remains.

---

## 📌 Validation Context
- **Feature/Epic:** Pastille 0.1.0 — ui-user widget pack in the spirit of the Mushroom cards (written from scratch): light (switch row + level bar), cover (icon buttons + opening bar), garage door (icon buttons + confirmation), trigger, sensors, modes as chips.
- **Test Date:** 2026-10-06
- **Covers interface as of:** `packs-examples/pastille/index.js` 0.1.0, on ui-user SDK 1 (working tree 2026-10-06).
- **Standard version:** 2.2.0
- **Compliance Status:** ⚠️ CONDITIONAL — automated, keyboard-structure and contrast checks pass; assistive-technology checks are open.
- **Verification Independence:** self-reported — *who verified: Claude Code (Opus 5.5), the session that wrote the pack.*
- **Static gate (`verify-a11y.py`):** NOT RUN — reason: the gate checks a project's artifacts and source tree; the pack is a single module inside the ui-user repository, whose gate run covers `frontend/src` only.

**Profile:** Standard (AA), 44 × 44 px targets. **Guides loaded:** guide-form-controls (switch, slider), guide-modals (confirmation), guide-visual-perception.

## 1. Technical Verification (Automated & Semantics)
- [x] **Axe-Core / Lighthouse:** axe-core 4.11.0 (`wcag2a wcag2aa wcag21a wcag21aa wcag22aa best-practice` + `label-content-name-mismatch`): **0 violations** on `/`, `/lights`, `/openings`, `/climate`, `/services` (all 6 widget types) in the 4 theme combinations (Ardoise / Contraste × light / dark) — 20 runs, 2026-10-06.
- [x] **HTML Semantics:** native `button` (switch rows, icon buttons, chips) and `input type=range` (level bars); icons are `aria-hidden` SVG; no handler on non-interactive elements (code-read).
- [x] **Heading Hierarchy (H1-H6):** the pack adds no heading; the SDK confirmation dialog has its `h2`.

## 2. Tab Order and Focus Management
- [x] **Focus Indicator:** `:focus-visible` outline `var(--eria-focus-width)` in `var(--eria-focus)`, offset 2 px, never removed (code-read; ring colors measured in ui-user REPORT.md §4).
- [x] **Logical Navigation:** DOM order = visual order inside each card (row, buttons, bar); no `tabindex` (code-read).
- [x] **Captured Focus (Modals/Overlays):** the garage confirmation is the SDK's native modal dialog (`renderConfirm`), verified in ui-user REPORT.md §2.

## 3. Behavior and Task Return
- [ ] **Screen Reader Test:** not run — needs a person. **To run by Cédric** with VoiceOver + Safari (macOS, iOS): light switch row, level bar, cover icon buttons, garage confirmation, mode chips.
  - Pair(s) used: —
  - Who ran it and when: —
  - Scenarios run: *Expected behavior* of guide-form-controls (switch, slider) and guide-modals.
- [~] **Voice Control:** visible labels are the accessible names (switch rows, chips, bars: axe `label-content-name-mismatch` 0); icon buttons have no visible text and are named "Ouvrir / Arrêter / Fermer + name". Not run with a voice-control tool.
  - Tool or method: axe rule + reading accessible names.
- [x] **Interactive states inventoried:**
  | Component | States | How verified |
  | :--- | :--- | :--- |
  | Light switch row | off, on, unknown (disabled), offline (dimmed), guest (disabled, not dimmed) | rendered on the real house, code-read for offline/guest |
  | Level bar | value, dragging (fill follows), committed on release | code-read; real `fade`/`setPosition` not sent |
  | Cover / garage buttons | enabled, disabled; garage asks to confirm | code-read (confirmation is the SDK's) |
  | Mode chip | off, on (amber + check mark), unknown (disabled) | rendered; not toggled (real modes) |
  | Sensor | value, unknown, several readings | rendered on the real weather service |
- [x] **Status Change (`aria-live`):** the pack announces nothing itself (values pushed by the house are not announced, as in Standard); command errors go through the SDK's `eria-error` → the app's alert.
- [x] **Form Filling:** each bar is wrapped in its `<label>` ("Luminosité", "Ouverture") with `aria-valuetext`.

## 4. Visual Perception and Comprehension
- [x] **Text & UI Contrast:** text and borders come from the `--eria-*` theme variables (pairs in ui-user REPORT.md §4). Pairs specific to this pack — text on the 16 % muted tint of chips and icon buttons (`color-mix` computed per channel):
  | Pair | Foreground | Background | Ratio | Floor | Result |
  | :--- | :--- | :--- | ---: | ---: | :--- |
  | Chip text, Ardoise light | #1c2730 | #e3e5e7 | 12.03:1 | 4.5:1 | ✅ |
  | Chip text, Ardoise dark | #e3e9ed | #303940 | 9.61:1 | 4.5:1 | ✅ |
  | Chip text, Contraste light | #000000 | #dedede | 15.61:1 | 4.5:1 | ✅ |
  | Chip text, Contraste dark | #ffffff | #333333 | 12.63:1 | 4.5:1 | ✅ |
- [x] **Redundancy:** "on" = amber pastille **and** state text; active chip = amber **and** check mark **and** `aria-checked`; open cover = icon shape change **and** state text. Pastille icons are decorative.
- [x] **Scale / Zoom:** reflow at 320 CSS px and root text at 200 % on a 1280 px viewport: no horizontal scroll, no pack content outside its card, on `/`, `/lights`, `/openings`, `/climate`, `/services` (2026-10-06). Names wrap (never truncated); vertical layout under 10.5rem.

## 5. Time-Based Media and Motion
N/A — no time-based media; the pack has no transition or animation.

## 6. Cognitive Load and Flow
N/A — no multi-step flow, authentication or time limit in a widget.

---
## 📝 Assessment Notes or Known Blockers
- **Note 1:** borderless cards (Mushroom style): a card is identified by its content, not by a 3:1 boundary; the controls inside (rows, chips, buttons, bars) carry text or a high-contrast icon.
- **Note 2:** level bars restyled with `appearance: none`; in forced-colors mode they go back to the native slider (`appearance: auto`).
- **Note 3:** open checkpoints: screen reader, voice control, forced colors seen rendered.
