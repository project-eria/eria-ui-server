# A11y Verification Report — ui-user

This report compiles the compliance evidence for a given *Feature*, ensuring its development reached the "Certification Ready" definition.

> This report is a **versioned project record** — never add it to `.gitignore`. Evidence hidden from version control is not evidence: QA and leadership verify it before release, and audits read it after.

> **Marking legend (mandatory):**
> `[x]` verified, with the evidence described beside it · `[!]` verified and **failed** (fix it, or open an entry in `EXCEPTIONS.md`) · `[~]` partially verified, with what is missing written down · `[ ]` **not verified** — the reason MUST be written beside it.
> Marking `[x]` without reproducible evidence invalidates the whole report.

> **Headless agents.** An AI without a browser MUST still produce this report, marking `[ ]` every checkpoint that requires a browser or assistive technology, with the reason and who must run it. A partial, honest report is evidence; a missing report is not. Overall status is CONDITIONAL — never PASS — while any `[ ]` or `[!]` remains.

---

## 📌 Validation Context
- **Feature/Epic:** ui-user — household dashboard: sign-in (profile + PIN), predefined screens (Home, Lights, Openings, Climate, Modes, All), custom screens and their edit mode, profile management, preferences (widget pack), the **Standard** widget pack (light, shutter, garage door, sensors, trigger, modes, generic switch/number/value/button). Third-party widget packs are **out of this report**: each ships its own (PACKS.md); without one it is shown as unverified.
- **Test Date:** 2026-10-06
- **Covers interface as of:** working tree of `src/ui-user` on 2026-10-06 (pre-`v0.1.0`, repository not created yet), built with `pnpm build` and served by the Go backend on `localhost:8090` against the real house Things.
- **Standard version:** 2.2.0
- **Compliance Status:** ⚠️ CONDITIONAL — automated, keyboard and layout checks pass; the human assistive-technology checkpoints below are open.
- **Verification Independence:** fresh-context — *who verified: Claude Code general-purpose subagent (Opus 5.5), new context over the repository, 2026-10-06: static gate, contrast-check.py, source review, pnpm check/test, go test, axe-core 4.11.0 on the 9 screens (light + dark, edit mode, dialogs), keyboard-driven dialog focus/Esc checks (initial run + re-verification after fixes); no assistive technology, no sign-in. Reflow 320 px, 200 % text, text spacing and sign-in states were checked by the generating session only; the current-page underline color (CSS only) was changed after the re-verification.*
- **Static gate (`verify-a11y.py`):** PASS — *run on: 2026-10-06 (Python 3.13 via nix; the system Python 3.9 is too old for the script).*

**Profile:** Standard (AA), with 44 × 44 px targets (wall tablet, touch) — PLAN.md §4.
**Guides loaded:** guide-form-controls, guide-forms, guide-cognitive, guide-modals, guide-navigation, guide-toasts-notifications, guide-drag-drop, guide-responsive-mobile, guide-visual-perception.

## 1. Technical Verification (Automated & Semantics)
Evidence obtained via static validators to ensure a structural technical baseline.
- [x] **Axe-Core / Lighthouse:** axe-core 4.11.0, tags `wcag2a wcag2aa wcag21a wcag21aa wcag22aa best-practice experimental` + `label-content-name-mismatch` enabled: **0 violations** on the 9 screens (`/`, `/lights`, `/openings`, `/climate`, `/modes`, `/services`, `/screens`, `/screens/1`, `/profiles`), light and dark; also on the sign-in screen, on `/screens/1` in edit mode and with the card dialog open. axe runs into widget shadow roots. Lot 10 (2026-10-06): `/preferences` and the screen pack selector added, 0 violations on 7 screens re-run (incl. `/lights` rendered by the example pack Contour, for information).
- [x] **HTML Semantics:** every action is a native `button`/`a`/`input`/`select`; switches are `button role="switch"`; dialogs are native `<dialog>`; no click handler on `div`/`span` (code-read of `src/widgets/*.ts`, `src/routes/**`).
- [x] **Heading Hierarchy (H1-H6):** one `h1` per screen, sections and title cards `h2`, dialogs `h2` (axe `heading-order`, `page-has-heading-one`: pass).

## 2. Tab Order and Focus Management
Validation purely by keyboard (without mouse usage).
- [x] **Focus Indicator:** `:focus-visible` 3 px outline, offset 2 px, never removed (`outline: none` removed from the skip target `main` after the static gate flagged it). Ring vs page background: 5.04:1 light, 8.22:1 dark (table §4).
- [x] **Logical Navigation:** skip link first, then navigation, then content in DOM order; no positive `tabindex` in the source; grid without dense packing so visual order = DOM order. Verified with `Tab` on Home and Lights (2026-10-05) and by DOM order elsewhere.
- [x] **Captured Focus (Modals/Overlays):** native `showModal()` dialogs (Details, garage confirmation, card settings, card add): focus lands inside (Details: on its heading, so a stray Enter cannot drive a device), Esc closes, focus returns to the opener. Two defects found by the independent verifier and fixed: focus lost when switching between the Add and Settings card dialogs; a second Esc discarding unsaved settings.

## 3. Behavior and Task Return
Critical functional paths and Screen Reader validation.
- [ ] **Screen Reader Test:** not run — needs a person with assistive technology. **To run by Cédric:** VoiceOver + Safari (macOS) and VoiceOver + Safari (iOS), on the wall-tablet-like layout too.
  - Pair(s) used: —
  - Who ran it and when: —
  - Scenarios run: *Expected behavior* of guide-form-controls (switch, slider, radio group), guide-modals, guide-navigation (skip link, landmarks, route change), guide-forms + guide-cognitive (sign-in, profile form), guide-toasts-notifications (command error alert), guide-drag-drop (Move up / Move down announcements).
- [~] **Voice Control:** accessible names checked against visible labels by axe `label-content-name-mismatch` (0 violations) and by reading: repeated commands carry the visible verb first ("Ouvrir Volet Séjour"). Not run with a voice-control tool — **to run by Cédric** with macOS/iOS Voice Control.
  - Tool or method: axe rule + reading accessible names against visible labels.
- [x] **Interactive states inventoried:**
  | Component | States | How verified |
  | :--- | :--- | :--- |
  | Light switch / mode switch | off, on, unknown, offline (disabled, dimmed), guest (disabled, not dimmed) | navigated (keyboard, real lamp and real mode) |
  | Slider (dimmer, shutter opening) | value, unknown, offline | code-read; real `fade`/`setPosition` not tried |
  | Shutter / garage tile and card | open, closed, open at n %, moving, ajar | navigated (garage confirmation opened and cancelled, door untouched) |
  | Details dialog | closed, open, closed by Esc | navigated (keyboard) |
  | Card settings / add dialog | open, changed, Esc with changes (confirm), Esc unchanged | scripted in the browser |
  | Edit mode | view, editing, saving, conflict (409), unsaved + leave (confirm) | scripted in the browser |
  | Sign-in | empty, wrong PIN, locked (n min), success | navigated (keyboard + typed test PINs, local) |
  | Command error alert | shown, dismissed | code-read + backend 403 observed |
  | Reconnecting notice | connected, reconnecting | code-read |
- [x] **Status Change (`aria-live`):** sign-in errors and lockout in `role="alert"`; command failures in an always-present `role="alert"` container; edit-mode entry/exit, card added/moved/removed/updated in a polite `role="status"`; "reconnecting" in `role="status"` — checked by reading the region text after each action (2026-10-06).
- [x] **Form Filling:** every input has a `<label>` (wrapping or `for`/`id`); PIN help and error linked by `aria-describedby`; radio group in `fieldset`/`legend` (axe `label`, `label-title-only`: pass).

## 4. Visual Perception and Comprehension
Tests validating contrast and visual structure (color-independent).
- [x] **Text & UI Contrast:** computed with `tools/contrast-check.py` (A11Y.md repository, 2026-10-06). Two themes ship with the UI (lot 11): **Ardoise** (Standard AA) and **Contraste** (text ≥ 7:1, Shield profile, 4 px focus), each light and dark; rows prefixed "Contraste" belong to it, the others to Ardoise.
  | Pair | Foreground | Background | Ratio | Floor | Result |
  | :--- | :--- | :--- | ---: | ---: | :--- |
  | Body text on card (light) | #1c2730 | #ffffff | 15.19:1 | 4.5:1 | ✅ |
  | Body text on page (light) | #1c2730 | #e6eaed | 12.56:1 | 4.5:1 | ✅ |
  | Secondary text on card (light) | #4f5d68 | #ffffff | 6.78:1 | 4.5:1 | ✅ |
  | Secondary text on page (light) | #4f5d68 | #e6eaed | 5.60:1 | 4.5:1 | ✅ |
  | Text on lit tile (light) | #1c2730 | #f2b33d | 8.17:1 | 4.5:1 | ✅ |
  | Error text on card (light) | #a32020 | #ffffff | 7.54:1 | 4.5:1 | ✅ |
  | Control border on card (light) | #6f7d86 | #ffffff | 4.24:1 | 3:1 | ✅ |
  | Control border on page (light) | #6f7d86 | #e6eaed | 3.50:1 | 3:1 | ✅ |
  | Focus ring on page (light) | #1d5fbf | #e6eaed | 5.04:1 | 3:1 | ✅ |
  | Body text on card (dark) | #e3e9ed | #1a2229 | 13.14:1 | 4.5:1 | ✅ |
  | Body text on page (dark) | #e3e9ed | #11171c | 14.74:1 | 4.5:1 | ✅ |
  | Secondary text on card (dark) | #a3b0b9 | #1a2229 | 7.26:1 | 4.5:1 | ✅ |
  | Secondary text on page (dark) | #a3b0b9 | #11171c | 8.14:1 | 4.5:1 | ✅ |
  | Text on lit tile (dark) | #11171c | #f5be4f | 10.63:1 | 4.5:1 | ✅ |
  | Error text on card (dark) | #ff9b9b | #1a2229 | 7.98:1 | 4.5:1 | ✅ |
  | Control border on card (dark) | #6f7e88 | #1a2229 | 3.84:1 | 3:1 | ✅ |
  | Control border on page (dark) | #6f7e88 | #11171c | 4.31:1 | 3:1 | ✅ |
  | Focus ring on page (dark) | #7fb0ff | #11171c | 8.22:1 | 3:1 | ✅ |
  | Primary button text (light) | #ffffff | #1c2730 | 15.19:1 | 4.5:1 | ✅ |
  | Primary button text (dark) | #1a2229 | #e3e9ed | 13.14:1 | 4.5:1 | ✅ |
  | Error text on page (light) | #a32020 | #e6eaed | 6.23:1 | 4.5:1 | ✅ |
  | Error text on page (dark) | #ff9b9b | #11171c | 8.95:1 | 4.5:1 | ✅ |
  | Focus ring on card / in dialogs (light) | #1d5fbf | #ffffff | 6.10:1 | 3:1 | ✅ |
  | Focus ring on card / in dialogs (dark) | #7fb0ff | #1a2229 | 7.32:1 | 3:1 | ✅ |
  | Current-page underline on nav (light) | #1c2730 | #ffffff | 15.19:1 | 3:1 | ✅ |
  | Current-page underline on nav (dark) | #e3e9ed | #1a2229 | 13.14:1 | 3:1 | ✅ |
  | Contraste — text on card (light) | #000000 | #ffffff | 21.00:1 | 7:1 | ✅ |
  | Contraste — text on page (light) | #000000 | #ebebeb | 17.62:1 | 7:1 | ✅ |
  | Contraste — secondary text on card (light) | #2e2e2e | #ffffff | 13.58:1 | 7:1 | ✅ |
  | Contraste — secondary text on page (light) | #2e2e2e | #ebebeb | 11.39:1 | 7:1 | ✅ |
  | Contraste — text on lit tile (light) | #000000 | #ffd000 | 14.27:1 | 7:1 | ✅ |
  | Contraste — error text on card (light) | #8b0000 | #ffffff | 10.01:1 | 7:1 | ✅ |
  | Contraste — control border on card (light) | #5c5c5c | #ffffff | 6.69:1 | 3:1 | ✅ |
  | Contraste — control border on page (light) | #5c5c5c | #ebebeb | 5.61:1 | 3:1 | ✅ |
  | Contraste — focus ring on page (light) | #0033a0 | #ebebeb | 8.89:1 | 3:1 | ✅ |
  | Contraste — primary button text (light) | #ffffff | #000000 | 21.00:1 | 7:1 | ✅ |
  | Contraste — text on card (dark) | #ffffff | #141414 | 18.42:1 | 7:1 | ✅ |
  | Contraste — text on page (dark) | #ffffff | #000000 | 21.00:1 | 7:1 | ✅ |
  | Contraste — secondary text on card (dark) | #d4d4d4 | #141414 | 12.43:1 | 7:1 | ✅ |
  | Contraste — secondary text on page (dark) | #d4d4d4 | #000000 | 14.17:1 | 7:1 | ✅ |
  | Contraste — text on lit tile (dark) | #000000 | #ffd000 | 14.27:1 | 7:1 | ✅ |
  | Contraste — error text on card (dark) | #ff9e9e | #141414 | 9.33:1 | 7:1 | ✅ |
  | Contraste — control border on card (dark) | #a3a3a3 | #141414 | 7.30:1 | 3:1 | ✅ |
  | Contraste — control border on page (dark) | #a3a3a3 | #000000 | 8.33:1 | 3:1 | ✅ |
  | Contraste — focus ring on page (dark) | #9cc3ff | #000000 | 11.66:1 | 3:1 | ✅ |
  | Contraste — primary button text (dark) | #141414 | #ffffff | 18.42:1 | 7:1 | ✅ |
- [x] **Redundancy:** "on" is amber **and** a text state ("Allumée", "Activé") **and** `aria-checked`; offline is a text badge "Hors ligne"; errors are text with a 2 px border.
- [x] **Scale / Zoom:** root font size 200 % at 1280 px: no horizontal scroll, no widget content outside its card, all screens (2026-10-06). Reflow at 320 CSS px: no two-dimensional scrolling on the 9 screens (one fix made: long raw values now wrap). Side rail scrolls vertically when it outgrows the screen.

## 5. Time-Based Media and Motion
N/A — no time-based media. The only motion is the 0.2 s glow of a lit tile, disabled under `prefers-reduced-motion: reduce` (code-read: the transition sits inside `@media (prefers-reduced-motion: no-preference)`); nothing autoplays, moves for more than 5 s or flashes.

## 6. Cognitive Load and Flow
*Applies to any flow with more than one step, an authentication, or a time limit.*
- [x] **Nothing to remember:** sign-in = choose a visible profile + type a PIN; the PIN field accepts paste and declares `autocomplete="current-password"`; no CAPTCHA.
- [x] **Nothing to retype:** a single profile is preselected; a wrong PIN keeps the chosen profile; the edit-mode draft survives every action until Save or Cancel, and leaving with a draft asks first.
- [x] **Help in the same place:** no help mechanism; field help (PIN length) sits under its field on both forms that take a PIN.
- [x] **Text spacing:** line height 1.5, paragraph 2 em, letters 0.12 em, words 0.16 em injected in the page and in every widget shadow root, at 320 px: nothing clipped or overlapping (2026-10-06; fix made: names are no longer truncated).
- [x] **Timing:** no time limit in a task. Session lasts 30 days. The PIN lockout (1 min, doubling up to 1 h after 5 wrong PINs) is a security mechanism; the wait is stated in minutes.
- [x] **Conflicting needs:** none recorded (A11Y-DECISIONS.md lists the pattern choices).

---
## 📝 Assessment Notes or Known Blockers
*Describe whether any exception behavior was detected or what measures were opened in `EXCEPTIONS.md`*

- **Note 1 — open human checkpoints:** screen reader (VoiceOver + Safari on macOS and iOS), voice control, and forced colors / Windows high contrast (the widgets have `forced-colors` rules — `Highlight` for "on" — never seen rendered). Until they are run the status stays CONDITIONAL.
- **Note 2 — fixed during this verification:** card settings dialog lost unsaved settings on Esc (now asks, on every Esc); focus lost when switching card dialogs; Details dialog focused the lamp switch first (now its heading); unknown on/off state announced "off" (switch now disabled until known); `/services` loading/error regions inserted on the fly (now always present); current-page underline in amber at 1.86:1 (now ink); long raw values widened the page at 320 px (now wrap); names were truncated in narrow cards (now wrap, rows grow); side rail could hide links at 200 % (now scrolls); `outline: none` on the skip target (removed).
- **Note 3 — out of the product:** `/demo` (widget showcase, not in the navigation) uses fixed-height cells on purpose and can overflow at large text sizes.
- **Note 4 — no EXCEPTIONS.md:** no deviation was accepted.
- **Note 6 — themes (lot 11):** axe 0 violations on 6 screens for each of the 4 theme × scheme combinations (24 runs, 2026-10-06, generating session only). A custom screen's theme applies to the whole page while it is shown; the last theme is remembered locally and applied before the first paint (no flash of another theme).
- **Note 5 — widget packs (lot 10):** a pack is third-party code with its own accessibility; the app guarantees the fallback (Standard always selectable, broken pack → Standard + status message, tested) and labels packs without report as unverified. The lot-10 additions (Preferences page, pack selector) were checked by the generating session only.
