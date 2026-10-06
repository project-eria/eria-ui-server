// Capability widgets: one per ERIA capability (eria-core model catalog).
// Commands always go through actions: gateway-knx's property writes only
// change the cached value, they don't drive the device.
import { html, nothing, type TemplateResult } from 'lit';
import { EriaWidget, widgetStyles } from './base';
import { formatParts, formatValue } from './format';
import { m } from '../lib/paraglide/messages';

/** Light, with or without dimmer. Tile: a switch, plus details when dimmable. */
export class EriaLight extends EriaWidget {
	static styles = widgetStyles;

	protected watched() {
		return ['on', 'brightness'].filter((p) => p in this.schemas);
	}

	get #dimmable() {
		return 'brightness' in this.schemas;
	}

	#state() {
		const on = this.values.on;
		if (on === undefined) return m.no_value();
		if (!on) return m.light_off();
		return this.#dimmable && typeof this.values.brightness === 'number'
			? m.light_on_at({ level: formatValue(this.values.brightness, { unit: '%' }) })
			: m.light_on();
	}

	#switch() {
		const on = this.values.on === true;
		return html`<button
			class="tile ${on ? 'lit' : ''}"
			role="switch"
			aria-checked=${on ? 'true' : 'false'}
			?disabled=${this.locked || this.values.on === undefined}
			@click=${() => this.act('toggle')}
		>
			<span class="label">${this.label}</span>
			<span class="state" aria-hidden="true">${this.#state()}</span>
		</button>`;
	}

	#full() {
		const level = this.values.brightness;
		const text = formatValue(level, { unit: '%' });
		return html`<div class="card">
			${this.#switch()}
			${this.#dimmable
				? html`<label class="muted" for="level">${m.brightness()} : ${text}</label>
						<input
							id="level"
							type="range"
							min="0"
							max="100"
							step="1"
							aria-valuetext=${text}
							.value=${String(level ?? 0)}
							?disabled=${this.locked}
							@change=${(e: Event) => this.act('fade', Number((e.target as HTMLInputElement).value))}
						/>`
				: nothing}
		</div>`;
	}

	render() {
		if (this.variant === 'value') return valueCard(this.label, this.#state());
		if (this.variant === 'tile')
			return html`<div class="stack">${this.#switch()}${this.#dimmable ? this.renderDetails(() => this.#full()) : nothing}</div>`;
		return this.#full();
	}
}

/** Shutter, with or without position (position = % open, 100 = fully open). */
export class EriaShutter extends EriaWidget {
	static styles = widgetStyles;

	protected watched() {
		return ['open', 'position'].filter((p) => p in this.schemas);
	}

	#state() {
		const { open, position } = this.values;
		if (typeof position === 'number') {
			if (position <= 0) return m.shutter_closed();
			if (position >= 100) return m.shutter_open();
			return m.shutter_open_at({ level: formatValue(position, { unit: '%' }) });
		}
		if (open === undefined) return m.no_value();
		return open ? m.shutter_open() : m.shutter_closed();
	}

	#commands() {
		return html`<div class="row">
			${(['open', 'stop', 'close'] as const).map(
				(a) => html`<button ?disabled=${this.locked} @click=${() => this.act(a)}>
					${commandLabel[a]()}<span class="vh"> ${this.label}</span>
				</button>`
			)}
		</div>`;
	}

	#full() {
		const position = this.values.position;
		const text = formatValue(position, { unit: '%' });
		return html`<div class="card">
			${header(this.label, this.#state())} ${this.#commands()}
			${'position' in this.schemas
				? html`<label class="muted" for="position">${m.opening()} : ${text}</label>
						<input
							id="position"
							type="range"
							min="0"
							max="100"
							step="1"
							aria-valuetext=${text}
							.value=${String(position ?? 0)}
							?disabled=${this.locked}
							@change=${(e: Event) => this.act('setPosition', Number((e.target as HTMLInputElement).value))}
						/>`
				: nothing}
		</div>`;
	}

	render() {
		if (this.variant === 'value') return valueCard(this.label, this.#state());
		if (this.variant === 'tile') {
			const open = this.values.open === true;
			const action = open ? 'close' : 'open';
			return html`<div class="card">
				${header(this.label, this.#state())}
				<div class="row">
					<button ?disabled=${this.locked} @click=${() => this.act(action)}>
						${commandLabel[action]()}<span class="vh"> ${this.label}</span>
					</button>
					${this.renderDetails(() => this.#full())}
				</div>
			</div>`;
		}
		return this.#full();
	}
}

/** Garage door: asks for confirmation by default (set by widgetFor). */
export class EriaGarage extends EriaWidget {
	static styles = widgetStyles;

	protected watched() {
		return ['open', 'moving', 'closed'];
	}

	#state() {
		const { open, moving, closed } = this.values;
		if (moving) return m.garage_moving();
		if (closed) return m.garage_closed();
		if (open) return m.garage_open();
		if (closed === false) return m.garage_ajar();
		return m.no_value();
	}

	#button(action: 'open' | 'close') {
		return html`<button ?disabled=${this.locked} @click=${() => this.act(action)}>
			${commandLabel[action]()}<span class="vh"> ${this.label}</span>
		</button>`;
	}

	render() {
		const question = this.values.closed ? m.confirm_open({ label: this.label }) : m.confirm_close({ label: this.label });
		if (this.variant === 'value') return valueCard(this.label, this.#state());
		const buttons =
			this.variant === 'tile'
				? this.#button(this.values.closed ? 'open' : 'close')
				: html`${this.#button('open')}${this.#button('close')}`;
		return html`<div class="card">
				${header(this.label, this.#state())}
				<div class="row">${buttons}</div>
			</div>
			${this.renderConfirm(question)}`;
	}
}

/** Doorbell, motion... */
export class EriaTrigger extends EriaWidget {
	static styles = widgetStyles;

	protected watched() {
		return ['triggered'];
	}

	render() {
		const triggered = this.values.triggered;
		const state = triggered === undefined ? m.no_value() : triggered ? m.triggered() : m.idle();
		if (this.variant === 'value') return valueCard(this.label, state);
		return html`<div class="readout ${triggered ? 'lit' : ''}">
			<span class="label">${this.label}</span><span class="state">${state}</span>
		</div>`;
	}
}

/** Every sensor capability: shows its values (all, or the chosen `props`). */
export class EriaSensor extends EriaWidget {
	static properties = { ...EriaWidget.properties, props: { attribute: false } };
	static styles = widgetStyles;
	declare props?: string[];

	protected watched() {
		if (this.props?.length) return this.props;
		// Main reading first (wind speed before its direction).
		return Object.keys(this.schemas).sort((a, b) => Number(a === 'windAngle') - Number(b === 'windAngle'));
	}

	#text(name: string) {
		const value = this.values[name];
		const text = formatValue(value, this.schemaOf(name));
		return name === 'windAngle' && typeof value === 'number' ? `${compass(value)} (${text})` : text;
	}

	render() {
		const names = this.watched();
		if (this.variant === 'value') {
			const name = names[0];
			const reading =
				name === 'windAngle' ? this.#text(name) : formatParts(this.values[name], this.schemaOf(name));
			return valueCard(this.label, reading, propertyName(name));
		}
		if (this.variant === 'tile' && names.length === 1) {
			return html`<div class="readout">
				<span class="label">${this.label}<span class="vh"> ${propertyName(names[0])}</span></span>
				<span class="state">${this.#text(names[0])}</span>
			</div>`;
		}
		return html`<div class="card">
			<span class="label">${this.label}</span>
			<dl>
				${names.map((n) => html`<dt class="muted">${propertyName(n)}</dt><dd>${this.#text(n)}</dd>`)}
			</dl>
		</div>`;
	}
}

const commandLabel = { open: m.open_action, close: m.close_action, stop: m.stop_action };

const propertyNames: Record<string, () => string> = {
	temperature: m.prop_temperature,
	humidity: m.prop_humidity,
	pressure: m.prop_pressure,
	windStrength: m.prop_windStrength,
	windAngle: m.prop_windAngle,
	liters: m.prop_liters,
	uvIndex: m.prop_uvIndex,
	ph: m.prop_ph,
	orp: m.prop_orp,
	volts: m.prop_volts
};

export const propertyName = (name: string) => propertyNames[name]?.() ?? name;

const compassPoints = [m.dir_n, m.dir_ne, m.dir_e, m.dir_se, m.dir_s, m.dir_sw, m.dir_w, m.dir_nw];

/** Wind direction in words (8 points). */
export function compass(degrees: number): string {
	return compassPoints[Math.round((((degrees % 360) + 360) % 360) / 45) % 8]();
}

function header(label: string, state: string) {
	return html`<div class="tile-head">
		<span class="label">${label}</span>
		<span class="state muted">${state}</span>
	</div>`;
}

/** "Value only" variant: the reading, large (unit smaller), with its name. */
function valueCard(label: string, value: string | { text: string; unit: string }, detail?: string): TemplateResult {
	const { text, unit } = typeof value === 'string' ? { text: value, unit: '' } : value;
	return html`<div class="card">
		<span class="label">${detail ? html`${detail} <span class="muted">${label}</span>` : label}</span>
		<span class="big" style="--chars: ${text.length + unit.length / 2}"
			>${text}${unit ? html` <span class="unit">${unit}</span>` : nothing}</span
		>
	</div>`;
}
