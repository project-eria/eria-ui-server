// Generic widgets, chosen from the data schema when no capability is known.
import { html } from 'lit';
import { EriaWidget, widgetStyles } from './base';
import { formatValue } from './format';
import { m } from '../lib/paraglide/messages';

/** Writable boolean: APG switch. */
export class EriaSwitch extends EriaWidget {
	static styles = widgetStyles;

	render() {
		const value = this.values[this.property!];
		const on = value === true;
		return html`<button
			class="tile ${on ? 'lit' : ''}"
			role="switch"
			aria-checked=${on ? 'true' : 'false'}
			?disabled=${this.locked || value === undefined}
			@click=${() => this.run(() => this.thing!.write(this.property!, !on))}
		>
			<span class="label">${this.label}</span>
			<span class="state" aria-hidden="true">
				${value === undefined ? m.no_value() : on ? m.state_on() : m.state_off()}
			</span>
		</button>`;
	}
}

/** Writable number: a slider when bounded, a number field otherwise. Sent on commit, not while dragging. */
export class EriaNumber extends EriaWidget {
	static styles = widgetStyles;

	render() {
		const { minimum, maximum } = this.schema;
		const value = this.values[this.property!];
		const text = formatValue(value, this.schema);
		const commit = (e: Event) => {
			const v = Number((e.target as HTMLInputElement).value);
			this.run(() => this.thing!.write(this.property!, this.schema.type === 'integer' ? Math.round(v) : v));
		};
		const bounded = minimum !== undefined && maximum !== undefined;
		return html`<div class="field">
			<span class="label" id="label">${this.label}</span>
			<span class="value" aria-hidden="true">${text}</span>
			${bounded
				? html`<input
						type="range"
						aria-labelledby="label"
						aria-valuetext=${text}
						min=${minimum}
						max=${maximum}
						step=${this.schema.type === 'integer' ? 1 : 'any'}
						.value=${String(value ?? minimum)}
						?disabled=${this.locked}
						@change=${commit}
					/>`
				: html`<input
						type="number"
						aria-labelledby="label"
						.value=${value === undefined ? '' : String(value)}
						?disabled=${this.locked}
						@change=${commit}
					/>`}
		</div>`;
	}
}

/** Read-only value of any type. */
export class EriaValue extends EriaWidget {
	static styles = widgetStyles;

	render() {
		return html`<div class="readout">
			<span class="label">${this.label}</span>
			<span class="value">${formatValue(this.values[this.property!], this.schema)}</span>
		</div>`;
	}
}

/** Action without input. */
export class EriaButton extends EriaWidget {
	static properties = { ...EriaWidget.properties, action: {} };
	static styles = widgetStyles;
	declare action?: string;

	render() {
		return html`<button
			class="tile"
			?disabled=${this.locked}
			@click=${() => this.run(() => this.thing!.invoke(this.action!))}
		>
			<span class="label">${this.label}</span>
		</button>`;
	}
}
