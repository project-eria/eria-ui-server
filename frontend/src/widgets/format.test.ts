import { expect, test } from 'vitest';
import { formatValue } from './format';

test('formatValue', () => {
	expect(formatValue(24.75, { unit: '°C' }, 'fr')).toBe('24,8 °C');
	expect(formatValue(270, { unit: '°' }, 'fr')).toBe('270°');
	expect(formatValue(true, {}, 'fr')).toBe('Oui');
	expect(formatValue(['monday', 'weekday'], {}, 'fr')).toBe('monday et weekday');
	expect(formatValue('2026-10-05T19:26:45+02:00', {}, 'en')).toMatch(/2026/);
	expect(formatValue('auto', {}, 'fr')).toBe('auto');
	expect(formatValue(undefined, {}, 'fr')).toBe('Pas encore de valeur');
});
