// Which widget renders a capability: the Standard pack (built in), or a
// widget pack loaded at run time (PACKS.md). Pure data: no DOM needed.
import type { Variant } from './base';

/** Card size in grid cells: [width, height]. 1 × 1 only where the text is short (sensor values). */
export type Size = [number, number];
export type Sizes = Partial<Record<Variant, Size[]>>;

export interface CapabilityWidget {
	tag: string;
	/** Variants the widget offers, with their allowed sizes (first = default). */
	sizes: Sizes;
	confirm?: boolean;
}

/** A widget pack as listed by GET /api/packs. */
export interface PackInfo {
	id: string;
	name: string;
	version: string;
	sdk: number;
	entry: string;
	report?: string;
	widgets: Record<string, { tag: string; sizes?: Sizes }>;
	error?: string;
}

const sensor: CapabilityWidget = {
	tag: 'eria-sensor',
	sizes: { value: [[2, 1], [1, 1], [2, 2]], tile: [[2, 1]], full: [[4, 2]] }
};
const shutter: CapabilityWidget = {
	tag: 'eria-shutter',
	sizes: { tile: [[2, 2]], full: [[4, 3], [2, 4]], value: [[2, 1]] }
};

/** The Standard pack. */
export const catalog: Record<string, CapabilityWidget> = {
	LightBasic: { tag: 'eria-light', sizes: { tile: [[2, 1]], value: [[2, 1]] } },
	LightDimmer: { tag: 'eria-light', sizes: { tile: [[2, 2]], full: [[4, 2]], value: [[2, 1]] } },
	ShutterBasic: shutter,
	ShutterPosition: shutter,
	GarageDoor: { tag: 'eria-garage', confirm: true, sizes: { tile: [[2, 2]], full: [[4, 2]], value: [[2, 1]] } },
	Trigger: { tag: 'eria-trigger', sizes: { tile: [[2, 1]], value: [[2, 1], [1, 1]] } },
	TemperatureSensor: sensor,
	VoltageSensor: sensor,
	HygrometerSensor: sensor,
	BarometerSensor: sensor,
	WindgaugeSensor: { ...sensor, sizes: { ...sensor.sizes, tile: [[2, 2]] } }, // two readings
	WaterMeter: sensor,
	UVSensor: sensor,
	PHSensor: sensor,
	ORPSensor: sensor,
	Contexts: { tag: 'eria-modes', sizes: { full: [[4, 2]] } }
};

/**
 * The widget of a capability in a pack, falling back to Standard when the
 * pack has none (or no pack is given). Confirmation defaults stay Standard's:
 * they are about the device, not about the look.
 */
export function resolve(capability: string, pack?: PackInfo): CapabilityWidget | undefined {
	const standard = catalog[capability];
	const own = pack?.widgets[capability];
	if (!own) return standard;
	return { tag: own.tag, sizes: own.sizes ?? standard?.sizes ?? { tile: [[2, 1]] }, confirm: standard?.confirm };
}
