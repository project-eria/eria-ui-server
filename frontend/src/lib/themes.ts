// Themes shipped with the UI (PLAN §5.3) and how the active one is chosen.
// Their values live in app.css; this only names them and picks one.
import { m } from './paraglide/messages.js';

export const themes = [
	{ id: 'ardoise', name: m.theme_ardoise },
	{ id: 'contraste', name: m.theme_contraste }
];

export const schemes = [
	{ id: 'auto', name: m.scheme_auto },
	{ id: 'light', name: m.scheme_light },
	{ id: 'dark', name: m.scheme_dark }
];

/** Screen's choice, else the profile's, else the default ("" means "not set"). */
export function pick(screen: string | undefined, profile: string | undefined, fallback: string): string {
	return screen || profile || fallback;
}

/** Applies a theme and scheme to the whole page (`<html>` attributes read by app.css). */
export function applyTheme(theme: string, scheme: string) {
	const root = document.documentElement;
	root.dataset.theme = theme;
	root.dataset.scheme = scheme;
	// Remembered for the next load (see app.html), so the page never flashes another theme.
	try {
		localStorage.setItem('eria-theme', JSON.stringify({ theme, scheme }));
	} catch {
		// private mode: the next load just starts with the default theme
	}
}
