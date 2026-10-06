// Contour — example widget pack (see PACKS.md): lights as a round outlined
// button beside their name. Everything else (shutters, sensors…) falls back to
// the Standard pack. Plain ES module, no build: copy this folder into the
// server's packs directory.
import { EriaWidget, css, html, formatValue, messages as m } from '/sdk/eria-widgets.js';

class ContourLight extends EriaWidget {
	static styles = css`
		:host {
			display: block;
			container-type: inline-size;
			color: var(--eria-ink, CanvasText);
		}
		.wrap {
			box-sizing: border-box;
			block-size: 100%;
			display: flex;
			align-items: center;
			gap: var(--eria-gap, 0.5rem);
			padding: var(--eria-pad, 0.625rem 0.875rem);
			border: 2px solid var(--eria-line, GrayText);
			border-radius: 2.5rem;
		}
		button {
			flex: none;
			inline-size: 2.75rem;
			block-size: 2.75rem;
			border: 2px solid var(--eria-ink, ButtonText);
			border-radius: 50%;
			background: transparent;
			cursor: pointer;
		}
		button[aria-checked='true'] {
			background: var(--eria-lit, Highlight);
			border-color: var(--eria-lit-ink, ButtonText);
		}
		button:disabled {
			cursor: not-allowed;
		}
		:host(:not([viewonly])) button:disabled {
			opacity: 0.6;
		}
		:focus-visible {
			outline: var(--eria-focus-width, 3px) solid var(--eria-focus, Highlight);
			outline-offset: 2px;
		}
		.text {
			display: grid;
			min-inline-size: 0;
		}
		.label {
			font-weight: 600;
			overflow-wrap: break-word;
		}
		.state {
			color: var(--eria-muted, GrayText);
			font-variant-numeric: tabular-nums;
		}
		@media (forced-colors: active) {
			button[aria-checked='true'] {
				background: Highlight;
			}
		}
	`;

	// Properties followed by the SDK base class; it calls back on every change.
	watched() {
		return ['on', 'brightness'].filter((p) => p in this.schemas);
	}

	#state() {
		const { on, brightness } = this.values;
		if (on === undefined) return m.no_value();
		if (!on) return m.light_off();
		return typeof brightness === 'number' ? m.light_on_at({ level: formatValue(brightness, { unit: '%' }) }) : m.light_on();
	}

	render() {
		const on = this.values.on === true;
		// The switch is named by the visible label beside it (label-in-name, SC 2.5.3).
		const button =
			this.variant === 'value'
				? ''
				: html`<button
						role="switch"
						aria-checked=${on ? 'true' : 'false'}
						aria-labelledby="label"
						?disabled=${this.locked || this.values.on === undefined}
						@click=${() => this.act('toggle')}
					></button>`;
		return html`<div class="wrap">
			${button}
			<span class="text">
				<span class="label" id="label">${this.label}</span>
				<span class="state">${this.#state()}</span>
			</span>
		</div>`;
	}
}

if (!customElements.get('contour-light')) customElements.define('contour-light', ContourLight);
