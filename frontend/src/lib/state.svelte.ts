// App-wide reactive state, fed by the backend client.
import { client, type Profile, type ScreenData, type Service } from './client/eria';
import { setWidgetOptions, type PackInfo } from '../widgets';
import { ensurePack, loadPacks } from './packs';

export const app = $state({
	/** undefined: not known yet; null: nobody signed in. */
	me: undefined as Profile | null | undefined,
	services: [] as Service[],
	screens: [] as ScreenData[],
	/** Widget packs installed on the server, those loaded, and why some failed. */
	packs: [] as PackInfo[],
	loadedPacks: [] as string[],
	packErrors: {} as Record<string, string>,
	loading: true,
	error: '',
	connected: true
});

export async function loadServices() {
	try {
		const [services, modes] = await Promise.all([client.services(), client.modes()]);
		setWidgetOptions({ modes, readonly: app.me?.role === 'guest' }); // before the services render
		app.services = services;
		app.error = '';
	} catch (e) {
		app.error = e instanceof Error ? e.message : String(e);
	} finally {
		app.loading = false;
	}
}

client.onThingsChanged = loadServices;
client.onConnection = (connected) => (app.connected = connected);
client.onStatus = (thing, online) => {
	for (const s of app.services) if (s.thing === thing) s.online = online;
};

/** Finds out who is signed in, then starts the live connection. */
export async function start() {
	try {
		app.me = await client.me();
	} catch {
		app.me = null;
		return;
	}
	client.connect();
	// The profile's pack first: widgets then appear directly in it, instead of
	// Standard ones swapped a moment later. ensurePack never throws (fallback).
	await loadPacks();
	await ensurePack(app.me?.pack);
	await Promise.all([loadServices(), loadScreens()]);
}

export async function loadScreens() {
	try {
		app.screens = await client.screens();
	} catch {
		// keep the last known list; a load error shows on the screen itself
	}
}

export async function signOut() {
	await client.logout().catch(() => {});
	app.me = null;
	app.services = [];
	app.screens = [];
}

client.onUnauthorized = () => {
	client.disconnect();
	app.me = null;
};
