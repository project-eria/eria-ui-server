import { expect, test } from 'vitest';
import { pick } from './themes';

test('screen choice over profile choice over default', () => {
	expect(pick('contraste', 'ardoise', 'ardoise')).toBe('contraste');
	expect(pick('', 'contraste', 'ardoise')).toBe('contraste');
	expect(pick(undefined, '', 'ardoise')).toBe('ardoise');
	expect(pick('', undefined, 'auto')).toBe('auto');
});
