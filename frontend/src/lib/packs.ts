// Widget packs at run time: list, load (ES module import), and pick the one
// for a screen. Loading never blocks the display: Standard widgets show until
// the pack is ready, and stay if it fails.
import { app } from './state.svelte';
import { client } from './client/eria';
import type { PackInfo } from '../widgets';
import { m } from './paraglide/messages.js';

const loading = new Map<string, Promise<void>>();

export async function loadPacks() {
	try {
		app.packs = await client.packs();
	} catch {
		app.packs = [];
	}
}

/** Imports a pack's module once; on failure the reason is kept for the UI. */
export function ensurePack(id: string | undefined): Promise<void> {
	if (!id || id === 'standard' || app.loadedPacks.includes(id)) return Promise.resolve();
	let done = loading.get(id);
	if (!done) {
		done = (async () => {
			const pack = app.packs.find((p) => p.id === id);
			try {
				if (!pack) throw new Error(m.pack_missing());
				if (pack.error) throw new Error(pack.error);
				await import(/* @vite-ignore */ `/packs/${pack.id}/${pack.entry}`);
				const missing = Object.values(pack.widgets).filter((w) => !customElements.get(w.tag));
				if (missing.length) throw new Error(m.pack_undefined_tags({ tags: missing.map((w) => w.tag).join(', ') }));
				app.loadedPacks = [...app.loadedPacks, id];
			} catch (e) {
				app.packErrors = { ...app.packErrors, [id]: e instanceof Error ? e.message : String(e) };
			}
		})();
		loading.set(id, done);
	}
	return done;
}

/** Pack of a screen: its own, else the profile's; undefined = Standard (or not loaded yet). */
export function packFor(screenPack?: string): PackInfo | undefined {
	const id = screenPack || app.me?.pack;
	return id && app.loadedPacks.includes(id) ? app.packs.find((p) => p.id === id) : undefined;
}
