// ERIA widgets: importing this module registers the custom elements.
import { client, type Service } from '../lib/client/eria';
import type { Variant } from './base';
import { resolve, type PackInfo } from './catalog';
import { EriaGarage, EriaLight, EriaSensor, EriaShutter, EriaTrigger } from './capabilities';
import { EriaButton, EriaNumber, EriaSwitch, EriaValue } from './generic';
import { EriaModes, type Mode } from './modes';
import { m } from '../lib/paraglide/messages';

const elements = {
	'eria-light': EriaLight,
	'eria-shutter': EriaShutter,
	'eria-garage': EriaGarage,
	'eria-trigger': EriaTrigger,
	'eria-sensor': EriaSensor,
	'eria-modes': EriaModes,
	'eria-switch': EriaSwitch,
	'eria-number': EriaNumber,
	'eria-value': EriaValue,
	'eria-button': EriaButton
};
for (const [tag, element] of Object.entries(elements)) {
	if (!customElements.get(tag)) customElements.define(tag, element);
}

export { catalog, resolve, type PackInfo, type Size, type Sizes } from './catalog';

export interface WidgetSpec {
	key: string;
	tag: string;
	props: Record<string, unknown>;
}

/** Page-wide options: modes from the backend config; read-only for guests. */
let options: { modes: Mode[]; readonly: boolean } = { modes: [], readonly: false };
export const setWidgetOptions = (o: typeof options) => (options = o);

/** The capability widget of a service (from `pack`, else Standard), or undefined for generic services. */
export function widgetFor(service: Service, variant?: Variant, pack?: PackInfo): WidgetSpec | undefined {
	const widget = resolve(service.capability, pack);
	if (!widget) return undefined;
	const allowed = Object.keys(widget.sizes) as Variant[];
	return {
		key: service.id,
		tag: widget.tag,
		props: {
			thing: client.thing(service.thing),
			schemas: service.properties,
			label: service.label,
			online: service.online,
			confirm: widget.confirm ?? false,
			viewonly: options.readonly,
			variant: variant && allowed.includes(variant) ? variant : allowed.includes('full') ? 'full' : allowed[0],
			...(service.capability === 'Contexts' ? { modes: options.modes, label: m.cap_Contexts() } : {})
		}
	};
}

/** Generic widgets for a service: one per property, one per input-less action. */
export function widgetsFor(service: Service, pack?: PackInfo): WidgetSpec[] {
	const capability = widgetFor(service, undefined, pack);
	if (capability) return [capability];
	const thing = client.thing(service.thing);
	const specs: WidgetSpec[] = [];
	for (const [name, schema] of Object.entries(service.properties)) {
		if (schema.uriVariables) continue; // parameterised: not relayed yet
		const writable = !schema.readOnly;
		const tag =
			writable && schema.type === 'boolean'
				? 'eria-switch'
				: writable && (schema.type === 'number' || schema.type === 'integer')
					? 'eria-number'
					: 'eria-value';
		const label = schema.title ?? name;
		specs.push({ key: `p:${name}`, tag, props: { thing, property: name, schema, label, online: service.online, viewonly: options.readonly } });
	}
	for (const [name, action] of Object.entries(service.actions)) {
		if (action.input === undefined) {
			const label = action.title ?? name;
			specs.push({
				key: `a:${name}`,
				tag: 'eria-button',
				props: { thing, action: name, label, online: service.online, viewonly: options.readonly }
			});
		}
	}
	return specs;
}
