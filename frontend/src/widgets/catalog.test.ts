import { expect, test } from 'vitest';
import { resolve, type PackInfo } from './catalog';

const contour: PackInfo = {
	id: 'contour',
	name: 'Contour',
	version: '1.0.0',
	sdk: 1,
	entry: 'index.js',
	widgets: {
		LightBasic: { tag: 'contour-light', sizes: { tile: [[1, 1]] } },
		GarageDoor: { tag: 'contour-garage' } // no sizes: Standard's
	}
};

test('a pack widget replaces Standard, other capabilities fall back to Standard', () => {
	expect(resolve('LightBasic', contour)).toMatchObject({ tag: 'contour-light', sizes: { tile: [[1, 1]] } });
	expect(resolve('ShutterPosition', contour)?.tag).toBe('eria-shutter');
	expect(resolve('LightBasic')?.tag).toBe('eria-light');
	expect(resolve('generic', contour)).toBeUndefined();
});

test('safety defaults stay Standard whatever the pack', () => {
	const garage = resolve('GarageDoor', contour)!;
	expect(garage.tag).toBe('contour-garage');
	expect(garage.confirm).toBe(true); // confirmation is about the device, not the look
	expect(garage.sizes).toEqual(resolve('GarageDoor')!.sizes);
});
