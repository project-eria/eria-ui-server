// Cards of custom screens (PLAN §5.1): stored as JSON in the screen layout.
import type { Service } from './client/eria';
import { sections } from './sections';
import type { Variant } from '../widgets/base';
import { resolve, widgetFor, type PackInfo, type Sizes, type WidgetSpec } from '../widgets';

export type SectionId = keyof typeof sections;

export type Card =
	| {
			id: string;
			kind: 'service';
			service: string;
			variant: Variant;
			w: number;
			h: number;
			/** Name on this card only. */
			label?: string;
			/** Readings shown (multi-value sensors). */
			props?: string[];
			/** Ask before acting. */
			confirm?: boolean;
	  }
	| { id: string; kind: 'section'; section: SectionId }
	| { id: string; kind: 'title'; text: string };

export const newId = () => crypto.randomUUID();

/** Variants a service offers in a pack, with their sizes. */
export function variantsOf(service: Service, pack?: PackInfo): Sizes {
	return resolve(service.capability, pack)?.sizes ?? {};
}

/** A new service card with the widget's default variant and size. */
export function serviceCard(service: Service, pack?: PackInfo): Card {
	const spec = widgetFor(service, undefined, pack)!;
	const variant = spec.props.variant as Variant;
	const [w, h] = variantsOf(service, pack)[variant]![0];
	return { id: newId(), kind: 'service', service: service.id, variant, w, h };
}

/** The widget of a service card, with the card's own settings applied. */
export function cardSpec(card: Card & { kind: 'service' }, service: Service, pack?: PackInfo): WidgetSpec | undefined {
	// A variant or size the pack doesn't offer falls back to its default one.
	const spec = widgetFor(service, card.variant, pack);
	if (!spec) return undefined;
	return {
		...spec,
		key: card.id,
		props: {
			...spec.props,
			...(card.label ? { label: card.label } : {}),
			...(card.props?.length ? { props: card.props } : {}),
			...(card.confirm !== undefined ? { confirm: card.confirm } : {})
		}
	};
}

/** Short name of a card, for button labels and announcements. */
export function cardName(card: Card, services: Service[]): string {
	if (card.kind === 'title') return card.text;
	if (card.kind === 'section') return sections[card.section].title();
	return card.label || services.find((s) => s.id === card.service)?.label || card.service;
}

/** Capabilities whose widget sends commands (a confirmation makes sense). */
export const actionable = new Set(['LightBasic', 'LightDimmer', 'ShutterBasic', 'ShutterPosition', 'GarageDoor', 'Contexts']);

/** Sensor capabilities with several readings to choose from. */
export const multiReading = (service: Service) =>
	resolve(service.capability)?.tag === 'eria-sensor' && Object.keys(service.properties).length > 1;
