// Predefined sections: ready-made blocks that fill themselves with the
// services of some capabilities. Predefined screens are lists of sections;
// custom screens (lot 7) will be able to embed them as cards.
import type { Variant } from '../widgets/base';
import { m } from './paraglide/messages.js';

export interface Section {
	id: string;
	title: () => string;
	capabilities: string[];
	variant: Variant;
}

export const sections = {
	modes: { id: 'modes', title: m.section_modes, capabilities: ['Contexts'], variant: 'full' },
	lights: { id: 'lights', title: m.section_lights, capabilities: ['LightBasic', 'LightDimmer'], variant: 'tile' },
	openings: {
		id: 'openings',
		title: m.section_openings,
		capabilities: ['ShutterBasic', 'ShutterPosition', 'GarageDoor'],
		variant: 'tile'
	},
	climate: {
		id: 'climate',
		title: m.section_climate,
		capabilities: ['TemperatureSensor', 'HygrometerSensor', 'BarometerSensor', 'WindgaugeSensor', 'UVSensor'],
		variant: 'value'
	}
} satisfies Record<string, Section>;
