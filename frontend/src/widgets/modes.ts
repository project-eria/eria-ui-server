// Modes: the contexts of manager-contexts that people switch on and off.
import { css, html } from 'lit';
import { EriaWidget, widgetStyles } from './base';
import { m } from '../lib/paraglide/messages';

export interface Mode {
	id: string;
	label: string;
}

// Set every day by manager-contexts itself: never offered as modes.
const daily = new Set(['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday', 'weekday', 'weekend']);

/** One switch per mode; active contexts missing from `modes` are shown too (by id).
 * The group name is not shown: the section heading around it says "Modes". */
export class EriaModes extends EriaWidget {
	static properties = { ...EriaWidget.properties, modes: { attribute: false } };
	static styles = [
		widgetStyles,
		css`
			ul {
				display: grid;
				grid-template-columns: repeat(auto-fill, minmax(min(100%, 9rem), 1fr));
				gap: 0.5rem;
				margin: 0;
				padding: 0;
				list-style: none;
			}
		`
	];
	declare modes: Mode[];

	constructor() {
		super();
		this.modes = [];
	}

	protected watched() {
		return ['actives'];
	}

	render() {
		const actives = Array.isArray(this.values.actives) ? (this.values.actives as string[]) : [];
		const known = new Set(this.modes.map((mode) => mode.id));
		const shown = [
			...this.modes,
			...actives.filter((id) => !known.has(id) && !daily.has(id)).map((id) => ({ id, label: id }))
		];
		return html`<div class="card" role="group" aria-labelledby="modes-title">
			<span class="vh" id="modes-title">${this.label}</span>
			<ul>
				${shown.map((mode) => {
					const on = actives.includes(mode.id);
					return html`<li>
						<button
							class="tile ${on ? 'lit' : ''}"
							role="switch"
							aria-checked=${on ? 'true' : 'false'}
							?disabled=${this.locked || this.values.actives === undefined}
							@click=${() => this.act(on ? 'unsetContext' : 'setContext', mode.id)}
						>
							<span class="label">${mode.label}</span>
							<span class="state" aria-hidden="true">${on ? m.mode_on() : m.mode_off()}</span>
						</button>
					</li>`;
				})}
			</ul>
		</div>`;
	}
}
