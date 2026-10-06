import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import type { Property, ThingHandle } from '../lib/client/eria';
import { m } from '../lib/paraglide/messages';

export interface WidgetErrorDetail {
	label: string;
	message: string;
}

export type Variant = 'tile' | 'full' | 'value';

/**
 * Base of every ERIA widget: follows some properties of a Thing and reports
 * failed commands with a bubbling `eria-error` event, that the page displays.
 * Widgets never talk to the network themselves.
 */
export class EriaWidget extends LitElement {
	static properties = {
		thing: { attribute: false },
		property: {},
		schema: { attribute: false },
		schemas: { attribute: false },
		label: {},
		online: { type: Boolean },
		// Lowercase on purpose: Svelte lowercases spread names on custom elements,
		// and `readonly` would be taken for the HTML attribute (set as "false" text).
		viewonly: { type: Boolean, reflect: true },
		variant: {},
		confirm: { type: Boolean },
		values: { state: true }
	};

	declare thing?: ThingHandle;
	/** Single followed property (generic widgets). */
	declare property?: string;
	declare schema: Partial<Property>;
	/** Schemas of the service's properties (capability widgets). */
	declare schemas: Record<string, Partial<Property>>;
	declare label: string;
	declare online: boolean;
	/** Display only (guest profiles): commands are disabled. */
	declare viewonly: boolean;
	declare variant: Variant;
	/** Ask before running the main command (garage door...). */
	declare confirm: boolean;
	declare values: Record<string, unknown>;

	#unsubscribe: (() => void)[] = [];
	#pending?: () => Promise<unknown>;

	constructor() {
		super();
		this.schema = {};
		this.schemas = {};
		this.label = '';
		this.online = true;
		this.viewonly = false;
		this.variant = 'full';
		this.confirm = false;
		this.values = {};
	}

	/** Commands can't be sent: Thing offline, or read-only profile. */
	protected get locked() {
		return !this.online || this.viewonly;
	}

	/** Properties to follow; capability widgets list theirs. */
	protected watched(): string[] {
		return this.property ? [this.property] : [];
	}

	protected updated(changed: PropertyValues<this>) {
		if (changed.has('thing') || changed.has('property')) this.#follow();
	}

	connectedCallback() {
		super.connectedCallback();
		if (this.hasUpdated) this.#follow(); // moved in the DOM
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		this.#stop();
	}

	#stop() {
		this.#unsubscribe.forEach((u) => u());
		this.#unsubscribe = [];
	}

	#follow() {
		this.#stop();
		const thing = this.thing;
		if (!thing) return;
		for (const name of this.watched()) {
			const set = (v: unknown) => (this.values = { ...this.values, [name]: v });
			if (this.schemaOf(name).observable === false) {
				this.run(async () => set(await thing.read(name)));
			} else {
				this.#unsubscribe.push(thing.observe(name, set));
			}
		}
	}

	protected schemaOf(name: string): Partial<Property> {
		return this.schemas[name] ?? (name === this.property ? this.schema : {});
	}

	/** Runs a command; a failure becomes an `eria-error` event. */
	protected async run(command: () => Promise<unknown>) {
		try {
			await command();
		} catch (e) {
			const detail: WidgetErrorDetail = { label: this.label, message: e instanceof Error ? e.message : String(e) };
			this.dispatchEvent(new CustomEvent('eria-error', { detail, bubbles: true, composed: true }));
		}
	}

	/** Invokes an action, after confirmation when the widget asks for it. */
	protected act(action: string, input?: unknown) {
		const command = () => this.thing!.invoke(action, input);
		if (!this.confirm) return this.run(command);
		this.#pending = command;
		this.dialog('confirm')?.showModal();
	}

	protected dialog(id: string) {
		return this.renderRoot.querySelector<HTMLDialogElement>(`#${id}`);
	}

	/** Confirmation dialog; the focus starts on "Cancel" (safe default). */
	protected renderConfirm(question: string): TemplateResult | typeof nothing {
		if (!this.confirm) return nothing;
		const close = (e: Event) => {
			const dialog = e.target as HTMLDialogElement;
			if (dialog.returnValue === 'ok' && this.#pending) this.run(this.#pending);
			this.#pending = undefined;
			dialog.returnValue = '';
		};
		return html`<dialog id="confirm" aria-labelledby="confirm-title" @close=${close}>
			<h2 id="confirm-title">${question}</h2>
			<form method="dialog" class="actions">
				<button value="cancel" autofocus>${m.cancel()}</button>
				<button value="ok" class="primary">${m.confirm_action()}</button>
			</form>
		</dialog>`;
	}

	/** "Details" button of a tile, opening the full widget in a modal dialog. */
	protected renderDetails(full: () => TemplateResult): TemplateResult {
		return html`<button class="secondary" @click=${() => this.dialog('details')?.showModal()}>
				${m.details()}<span class="vh"> ${this.label}</span>
			</button>
			<dialog id="details" aria-labelledby="details-title">
				<!-- First focus on the heading, not on a control: a stray Enter must not drive a device. -->
				<h2 id="details-title" tabindex="-1" autofocus>${this.label}</h2>
				${full()}
				<form method="dialog" class="actions">
					<button>${m.close_dialog()}</button>
				</form>
			</dialog>`;
	}
}

/** Shared look; colors come from --eria-* custom properties set by the page. */
export const widgetStyles = css`
	:host {
		display: block;
		container-type: inline-size;
		color: var(--eria-ink, CanvasText);
	}
	.tile,
	.readout,
	.field,
	.card {
		box-sizing: border-box;
		inline-size: 100%;
		block-size: 100%;
		min-block-size: 3rem;
		padding: var(--eria-pad, 0.625rem 0.875rem);
		border: 1px solid var(--eria-line, GrayText);
		border-radius: var(--eria-radius, 0.75rem);
		background: var(--eria-surface, Canvas);
		color: inherit;
		font: inherit;
		text-align: start;
	}
	.tile,
	.readout,
	.field {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}
	.stack {
		display: grid;
		grid-template-rows: 1fr auto;
		gap: var(--eria-gap, 0.5rem);
		block-size: 100%;
	}
	.tile-head {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 0.25rem 0.75rem;
	}
	button {
		font: inherit;
		color: inherit;
		cursor: pointer;
	}
	button:disabled {
		cursor: not-allowed;
		opacity: 0.6;
	}
	/* Read-only profile: states must stay fully legible, they are what a guest is here for. */
	:host([viewonly]) button:disabled {
		cursor: default;
		opacity: 1;
	}
	/* Command buttons: at least 44 × 44 px. */
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--eria-gap, 0.5rem);
	}
	.row button,
	.actions button,
	.secondary {
		min-block-size: 2.75rem;
		min-inline-size: 2.75rem;
		padding: 0.375rem 0.875rem;
		border: 1px solid var(--eria-line, GrayText);
		border-radius: calc(var(--eria-radius, 0.75rem) - 0.25rem);
		background: var(--eria-bg, Canvas);
	}
	.row button {
		flex: 1 1 auto;
	}
	.primary {
		background: var(--eria-ink, ButtonText) !important;
		color: var(--eria-surface, ButtonFace);
		border-color: transparent;
	}
	/* The one strong signal of the UI: something switched on glows like a lamp. */
	.lit {
		background: var(--eria-lit, Highlight);
		color: var(--eria-lit-ink, HighlightText);
		border-color: transparent;
		box-shadow: var(--eria-glow, 0 0 1.5rem -0.25rem var(--eria-lit, Highlight));
	}
	/* Only the glow fades in: background and text switch together, so the
	   contrast is never broken mid-transition. */
	@media (prefers-reduced-motion: no-preference) {
		.tile {
			transition: box-shadow 0.2s;
		}
	}
	:focus-visible {
		outline: var(--eria-focus-width, 3px) solid var(--eria-focus, Highlight);
		outline-offset: 2px;
	}
	.label {
		font-weight: 600;
	}
	.state,
	.value {
		font-variant-numeric: tabular-nums;
	}
	/* Long raw values (JSON, ids) wrap anywhere rather than widen the page (SC 1.4.10). */
	.value {
		min-inline-size: 0;
		overflow-wrap: anywhere;
	}
	.readout {
		flex-wrap: wrap;
	}
	.muted {
		color: var(--eria-muted, GrayText);
	}
	.unit {
		font-size: 0.6em;
	}
	.big {
		/* Shrinks with long readings (--chars) so they stay inside the cell. */
		font-size: clamp(0.875rem, min(18cqi, 150cqi / var(--chars, 5)), 3.25rem);
		font-weight: 600;
		line-height: 1.1;
		font-variant-numeric: tabular-nums;
	}
	.field {
		flex-wrap: wrap;
	}
	input[type='range'] {
		flex: 1 1 100%;
		inline-size: 100%;
		min-block-size: 2.75rem;
		margin: 0;
		accent-color: var(--eria-lit, Highlight);
	}
	.field input[type='number'] {
		inline-size: 7rem;
		min-block-size: 2.75rem;
		font: inherit;
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 0.25rem 1rem;
		margin: 0;
	}
	dd {
		margin: 0;
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	dialog {
		inline-size: min(28rem, calc(100vw - 2rem));
		padding: 1.25rem;
		border: 1px solid var(--eria-line, GrayText);
		border-radius: var(--eria-radius, 0.75rem);
		background: var(--eria-surface, Canvas);
		color: var(--eria-ink, CanvasText);
	}
	dialog::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	dialog h2 {
		margin: 0 0 1rem;
		font-size: 1.25rem;
	}
	dialog .card {
		border: 0;
		padding: 0;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		margin-block-start: 1rem;
	}
	.vh {
		position: absolute;
		inline-size: 1px;
		block-size: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
	/* Names are never truncated (SC 1.4.10, 1.4.12): they wrap, and the grid row grows. */
	.label {
		overflow-wrap: break-word;
		flex: 1 1 auto;
		min-inline-size: 0;
	}
	.state {
		flex: none;
	}
	.card:has(.big) {
		justify-content: space-between;
		gap: 0.25rem;
	}
	.card:has(.big) .label {
		flex: none;
		font-size: 0.875rem;
	}
	/* Two columns or less: tighter. */
	@container (max-width: 12rem) {
		.tile,
		.readout,
		.field,
		.card {
			padding: 0.5rem 0.625rem;
			gap: 0.5rem;
		}
	}
	/* Two narrow columns or less: state under the name instead of beside it. */
	@container (max-width: 10.5rem) {
		.tile,
		.readout {
			flex-direction: column;
			align-items: flex-start;
			justify-content: space-between;
			gap: 0.25rem;
		}
	}
	@media (forced-colors: active) {
		.lit {
			background: Highlight;
			color: HighlightText;
		}
		.primary {
			border: 2px solid ButtonText;
		}
	}
`;
