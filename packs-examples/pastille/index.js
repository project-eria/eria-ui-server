// Pastille — widget pack in the spirit of the Mushroom cards for Home
// Assistant (piitaya/lovelace-mushroom), written from scratch for ERIA:
// borderless cards, a round "pastille" icon tinted when active, name over
// state, thick bars for levels, icon buttons and chips. Plain ES module on the
// public SDK (PACKS.md); looks come only from the --eria-* variables, so the
// Ardoise and Contraste themes apply. Amber still only means "switched on".
import {
	EriaWidget,
	widgetStyles,
	css,
	html,
	svg,
	nothing,
	formatValue,
	propertyName,
	compass,
	messages as m
} from '/sdk/eria-widgets.js';

// --- icons: simple strokes drawn for this pack, decorative (aria-hidden) ---

const paths = {
	bulb: svg`<path d="M9 18h6M10 21h4"/><path d="M12 3a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 3z"/>`,
	shutter: svg`<rect x="4" y="3" width="16" height="18" rx="1"/><path d="M4 8h16M4 12h16M4 16h16"/>`,
	shutterOpen: svg`<rect x="4" y="3" width="16" height="18" rx="1"/><path d="M4 7h16"/>`,
	garage: svg`<path d="M3 21V9l9-6 9 6v12"/><path d="M7 21v-8h10v8M7 16h10"/>`,
	garageOpen: svg`<path d="M3 21V9l9-6 9 6v12"/><path d="M7 21v-8h10v8"/>`,
	bell: svg`<path d="M6 16v-5a6 6 0 0 1 12 0v5l2 2H4z"/><path d="M10 21h4"/>`,
	thermometer: svg`<path d="M14 14.8V5a2 2 0 0 0-4 0v9.8a4 4 0 1 0 4 0z"/>`,
	drop: svg`<path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z"/>`,
	gauge: svg`<path d="M4 18a8 8 0 1 1 16 0"/><path d="M12 18l4-5"/>`,
	wind: svg`<path d="M3 9h11a3 3 0 1 0-3-3"/><path d="M3 15h15a3 3 0 1 1-3 3"/>`,
	sun: svg`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`,
	bolt: svg`<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>`,
	flask: svg`<path d="M9 3h6M10 3v6l-5 9a2 2 0 0 0 2 3h10a2 2 0 0 0 2-3l-5-9V3"/>`,
	up: svg`<path d="m6 15 6-6 6 6"/>`,
	down: svg`<path d="m6 9 6 6 6-6"/>`,
	stop: svg`<rect x="7" y="7" width="10" height="10" rx="1"/>`,
	check: svg`<path d="m5 12 5 5 9-10"/>`
};

const icon = (name) =>
	html`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${paths[name]}</svg>`;

const sensorIcon = { temperature: 'thermometer', humidity: 'drop', pressure: 'gauge', windStrength: 'wind', windAngle: 'wind', liters: 'drop', uvIndex: 'sun', volts: 'bolt', ph: 'flask', orp: 'flask' };

// --- shared look ---

const styles = css`
	:host {
		display: block;
		container-type: inline-size;
		color: var(--eria-ink, CanvasText);
	}
	.p-card {
		box-sizing: border-box;
		block-size: 100%;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 0.625rem;
		padding: 0.75rem;
		border-radius: var(--eria-radius, 0.75rem);
		background: var(--eria-surface, Canvas);
		box-shadow: 0 1px 3px rgb(0 0 0 / 0.14);
	}
	.p-row {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		min-inline-size: 0;
	}
	/* The whole row is the switch: a large target, as on the Mushroom cards. */
	button.p-row {
		box-sizing: border-box;
		inline-size: calc(100% + 0.5rem);
		min-block-size: 2.75rem;
		margin: -0.25rem;
		padding: 0.25rem;
		border: 0;
		border-radius: calc(var(--eria-radius, 0.75rem) - 0.25rem);
		background: none;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.shape {
		flex: none;
		display: grid;
		place-items: center;
		inline-size: 2.5rem;
		block-size: 2.5rem;
		border-radius: 50%;
		background: color-mix(in srgb, var(--eria-muted, GrayText) 16%, transparent);
		color: var(--eria-muted, GrayText);
	}
	/* Switched on: the only use of amber. */
	.shape.on {
		background: var(--eria-lit, Highlight);
		color: var(--eria-lit-ink, HighlightText);
	}
	/* Active but not "on" (shutter or door open): neutral tint. */
	.shape.active {
		background: color-mix(in srgb, var(--eria-ink, CanvasText) 14%, transparent);
		color: var(--eria-ink, CanvasText);
	}
	.shape svg {
		inline-size: 1.375rem;
		block-size: 1.375rem;
	}
	.info {
		display: grid;
		min-inline-size: 0;
	}
	.p-name {
		font-weight: 600;
		font-size: 0.9375rem;
		overflow-wrap: break-word;
	}
	.p-state {
		font-size: 0.8125rem;
		color: var(--eria-muted, GrayText);
		font-variant-numeric: tabular-nums;
	}
	.p-actions {
		display: flex;
		gap: 0.5rem;
	}
	.p-icon-btn {
		flex: 1;
		display: grid;
		place-items: center;
		min-block-size: 2.75rem;
		border: 0;
		border-radius: calc(var(--eria-radius, 0.75rem) - 0.25rem);
		background: color-mix(in srgb, var(--eria-muted, GrayText) 16%, transparent);
		color: var(--eria-ink, ButtonText);
		cursor: pointer;
	}
	.p-icon-btn svg {
		inline-size: 1.375rem;
		block-size: 1.375rem;
	}
	/* Thick level bar: native range (keyboard, screen readers), restyled. */
	.p-level {
		display: grid;
		gap: 0.25rem;
		font-size: 0.8125rem;
		color: var(--eria-muted, GrayText);
	}
	.p-bar {
		-webkit-appearance: none;
		appearance: none;
		inline-size: 100%;
		block-size: 2.75rem;
		margin: 0;
		border-radius: calc(var(--eria-radius, 0.75rem) - 0.25rem);
		background: linear-gradient(
			to right,
			var(--fill) var(--p, 0%),
			color-mix(in srgb, var(--eria-muted, GrayText) 16%, transparent) var(--p, 0%)
		);
		cursor: pointer;
	}
	.p-bar::-webkit-slider-thumb {
		-webkit-appearance: none;
		inline-size: 0.375rem;
		block-size: 2.75rem;
		border-radius: 0.25rem;
		background: var(--eria-ink, ButtonText);
	}
	.p-bar::-moz-range-thumb {
		inline-size: 0.375rem;
		block-size: 2.75rem;
		border: 0;
		border-radius: 0.25rem;
		background: var(--eria-ink, ButtonText);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		min-block-size: 2.75rem;
		padding-inline: 0.875rem;
		border: 0;
		border-radius: 1.375rem;
		background: color-mix(in srgb, var(--eria-muted, GrayText) 16%, transparent);
		color: var(--eria-ink, ButtonText);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
	.chip[aria-checked='true'] {
		background: var(--eria-lit, Highlight);
		color: var(--eria-lit-ink, HighlightText);
	}
	.chip svg {
		inline-size: 1rem;
		block-size: 1rem;
	}
	.readings {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem 1rem;
		margin: 0;
	}
	.readings div {
		display: flex;
		gap: 0.375rem;
	}
	.readings dt {
		color: var(--eria-muted, GrayText);
	}
	.readings dd {
		margin: 0;
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
	button:disabled,
	input:disabled {
		cursor: not-allowed;
	}
	:host(:not([viewonly])) :is(button, input):disabled {
		opacity: 0.6;
	}
	:focus-visible {
		outline: var(--eria-focus-width, 3px) solid var(--eria-focus, Highlight);
		outline-offset: 2px;
	}
	/* Two narrow columns or less: icon over the text, centered (Mushroom's vertical layout). */
	@container (max-width: 10.5rem) {
		.p-row {
			flex-direction: column;
			text-align: center;
			gap: 0.375rem;
		}
		button.p-row {
			justify-content: center;
		}
		.info {
			text-align: center;
		}
	}
	@media (forced-colors: active) {
		.shape.on,
		.chip[aria-checked='true'] {
			background: Highlight;
			color: HighlightText;
		}
		.p-bar {
			appearance: auto;
			background: none;
		}
		.p-icon-btn,
		.chip {
			border: 1px solid ButtonText;
		}
	}
`;

class Pastille extends EriaWidget {
	// widgetStyles: dialogs and helpers of the SDK. Its class names (.tile, .card,
	// .primary, .secondary, .label…) are taken: this pack's classes start with p-.
	static styles = [widgetStyles, styles];

	// Icon, name and state; the state is hidden inside a switch, whose name never changes.
	row(iconName, tint, secondary, inSwitch = false) {
		return html`<span class="shape ${tint}">${icon(iconName)}</span>
			<span class="info">
				<span class="p-name">${this.label}</span>
				<span class="p-state" aria-hidden=${inSwitch ? 'true' : 'false'}>${secondary}</span>
			</span>`;
	}

	level(name, value, fill, onCommit) {
		const text = formatValue(value, { unit: '%' });
		return html`<label class="p-level"
			>${name}
			<input
				class="p-bar"
				type="range"
				min="0"
				max="100"
				step="1"
				aria-valuetext=${text}
				style="--p: ${value ?? 0}%; --fill: ${fill}"
				.value=${String(value ?? 0)}
				?disabled=${this.locked}
				@input=${(e) => e.target.style.setProperty('--p', `${e.target.value}%`)}
				@change=${(e) => onCommit(Number(e.target.value))}
		/></label>`;
	}

	iconButton(name, verb, action) {
		return html`<button class="p-icon-btn" aria-label="${verb} ${this.label}" ?disabled=${this.locked} @click=${() => this.act(action)}>
			${icon(name)}
		</button>`;
	}
}

class PastilleLight extends Pastille {
	watched() {
		return ['on', 'brightness'].filter((p) => p in this.schemas);
	}

	render() {
		const { on, brightness } = this.values;
		const state =
			on === undefined ? m.no_value() : !on ? m.light_off() : typeof brightness === 'number' ? m.light_on_at({ level: formatValue(brightness, { unit: '%' }) }) : m.light_on();
		const tint = on ? 'on' : '';
		if (this.variant === 'value') return html`<div class="p-card"><div class="p-row">${this.row('bulb', tint, state)}</div></div>`;
		return html`<div class="p-card">
			<button
				class="p-row"
				role="switch"
				aria-checked=${on ? 'true' : 'false'}
				?disabled=${this.locked || on === undefined}
				@click=${() => this.act('toggle')}
			>
				${this.row('bulb', tint, state, true)}
			</button>
			${this.variant === 'full' && 'brightness' in this.schemas
				? this.level(m.brightness(), brightness, 'var(--eria-lit, Highlight)', (v) => this.act('fade', v))
				: nothing}
		</div>`;
	}
}

class PastilleCover extends Pastille {
	watched() {
		return ['open', 'position'].filter((p) => p in this.schemas);
	}

	render() {
		const { open, position } = this.values;
		const state =
			typeof position === 'number'
				? position <= 0 ? m.shutter_closed() : position >= 100 ? m.shutter_open() : m.shutter_open_at({ level: formatValue(position, { unit: '%' }) })
				: open === undefined ? m.no_value() : open ? m.shutter_open() : m.shutter_closed();
		const isOpen = open === true || (typeof position === 'number' && position > 0);
		const row = html`<div class="p-row">${this.row(isOpen ? 'shutterOpen' : 'shutter', isOpen ? 'active' : '', state)}</div>`;
		if (this.variant === 'value') return html`<div class="p-card">${row}</div>`;
		return html`<div class="p-card">
			${row}
			<div class="p-actions">
				${this.iconButton('up', m.open_action(), 'open')} ${this.iconButton('stop', m.stop_action(), 'stop')}
				${this.iconButton('down', m.close_action(), 'close')}
			</div>
			${this.variant === 'full' && 'position' in this.schemas
				? this.level(m.opening(), position, 'color-mix(in srgb, var(--eria-ink, CanvasText) 35%, transparent)', (v) => this.act('setPosition', v))
				: nothing}
		</div>`;
	}
}

class PastilleGarage extends Pastille {
	watched() {
		return ['open', 'moving', 'closed'];
	}

	render() {
		const { open, moving, closed } = this.values;
		const state = moving ? m.garage_moving() : closed ? m.garage_closed() : open ? m.garage_open() : closed === false ? m.garage_ajar() : m.no_value();
		const row = html`<div class="p-row">${this.row(closed ? 'garage' : 'garageOpen', closed === false ? 'active' : '', state)}</div>`;
		if (this.variant === 'value') return html`<div class="p-card">${row}</div>`;
		const question = closed ? m.confirm_open({ label: this.label }) : m.confirm_close({ label: this.label });
		return html`<div class="p-card">
				${row}
				<div class="p-actions">${this.iconButton('up', m.open_action(), 'open')} ${this.iconButton('down', m.close_action(), 'close')}</div>
			</div>
			${this.renderConfirm(question)}`;
	}
}

class PastilleTrigger extends Pastille {
	watched() {
		return ['triggered'];
	}

	render() {
		const t = this.values.triggered;
		const state = t === undefined ? m.no_value() : t ? m.triggered() : m.idle();
		return html`<div class="p-card"><div class="p-row">${this.row('bell', t ? 'active' : '', state)}</div></div>`;
	}
}

class PastilleSensor extends Pastille {
	static properties = { ...EriaWidget.properties, props: { attribute: false } };

	watched() {
		if (this.props?.length) return this.props;
		return Object.keys(this.schemas).sort((a, b) => Number(a === 'windAngle') - Number(b === 'windAngle'));
	}

	text(name) {
		const v = this.values[name];
		const t = formatValue(v, this.schemas[name] ?? {});
		return name === 'windAngle' && typeof v === 'number' ? `${compass(v)} (${t})` : t;
	}

	render() {
		const names = this.watched();
		const main = names[0];
		if (this.variant === 'full' && names.length > 1) {
			return html`<div class="p-card">
				<div class="p-row">${this.row(sensorIcon[main] ?? 'gauge', '', propertyName(main))}</div>
				<dl class="readings">
					${names.map((n) => html`<div><dt>${propertyName(n)}</dt><dd>${this.text(n)}</dd></div>`)}
				</dl>
			</div>`;
		}
		// The reading's name with its value: several cards of one Thing stay distinct.
		const secondary = names.map((n) => `${propertyName(n)} : ${this.text(n)}`).join(', ');
		return html`<div class="p-card"><div class="p-row">${this.row(sensorIcon[main] ?? 'gauge', '', secondary)}</div></div>`;
	}
}

// Modes as chips: one switch each; the visible check mark doubles the color.
const daily = new Set(['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday', 'weekday', 'weekend']);

class PastilleModes extends Pastille {
	static properties = { ...EriaWidget.properties, modes: { attribute: false } };

	watched() {
		return ['actives'];
	}

	render() {
		const actives = Array.isArray(this.values.actives) ? this.values.actives : [];
		const modes = this.modes ?? [];
		const known = new Set(modes.map((x) => x.id));
		const shown = [...modes, ...actives.filter((id) => !known.has(id) && !daily.has(id)).map((id) => ({ id, label: id }))];
		return html`<div class="p-card" role="group" aria-labelledby="modes-title">
			<span class="vh" id="modes-title">${this.label}</span>
			<ul class="chips">
				${shown.map((mode) => {
					const on = actives.includes(mode.id);
					return html`<li>
						<button
							class="chip"
							role="switch"
							aria-checked=${on ? 'true' : 'false'}
							?disabled=${this.locked || this.values.actives === undefined}
							@click=${() => this.act(on ? 'unsetContext' : 'setContext', mode.id)}
						>
							${on ? icon('check') : nothing}${mode.label}
						</button>
					</li>`;
				})}
			</ul>
		</div>`;
	}
}

const elements = {
	'pastille-light': PastilleLight,
	'pastille-cover': PastilleCover,
	'pastille-garage': PastilleGarage,
	'pastille-trigger': PastilleTrigger,
	'pastille-sensor': PastilleSensor,
	'pastille-modes': PastilleModes
};
for (const [tag, element] of Object.entries(elements)) {
	if (!customElements.get(tag)) customElements.define(tag, element);
}
