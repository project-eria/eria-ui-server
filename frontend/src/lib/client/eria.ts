import type { PackInfo } from '../../widgets/catalog';
// Client of the ui-user backend: REST for services and commands, one shared
// WebSocket for every observed property.

export interface Property {
	title?: string;
	type?: string;
	unit?: string;
	minimum?: number;
	maximum?: number;
	enum?: unknown[];
	readOnly: boolean;
	observable: boolean;
	uriVariables?: Record<string, unknown>;
}

export interface Action {
	title?: string;
	input?: unknown;
}

export interface Service {
	id: string;
	thing: string;
	capability: string;
	label: string;
	online: boolean;
	properties: Record<string, Property>;
	actions: Record<string, Action>;
}

export type Listener = (value: unknown) => void;

export type Role = 'admin' | 'member' | 'guest';

export interface Profile {
	id: number;
	name: string;
	role?: Role;
	/** Widget pack chosen by the profile; empty = Standard. */
	pack?: string;
	/** Theme and color scheme (auto | light | dark); empty = defaults. */
	theme?: string;
	scheme?: string;
}

export interface ScreenData {
	id: number;
	title: string;
	shared: boolean;
	/** Shared screens: minimum role to edit them. */
	editRole?: 'admin' | 'member';
	position: number;
	version: number;
	canEdit: boolean;
	/** Widget pack, theme and color scheme of the screen; empty = the profile's. */
	pack: string;
	theme: string;
	scheme: string;
	/** Cards, see #lib/cards.ts. */
	layout: unknown[];
}

/** A failed HTTP call: status, and seconds to wait when the profile is locked. */
export class HttpError extends Error {
	constructor(
		message: string,
		readonly status: number,
		readonly retryAfter = 0
	) {
		super(message);
	}
}

/** What a widget receives: everything it may do with one Thing. */
export interface ThingHandle {
	readonly id: string;
	read(property: string): Promise<unknown>;
	write(property: string, value: unknown): Promise<void>;
	invoke(action: string, input?: unknown): Promise<unknown>;
	/** Calls back with the current value, then every change; returns the unsubscribe. */
	observe(property: string, listener: Listener): () => void;
}

type ServerMsg =
	| { thing: string; property: string; value: unknown }
	| { thing: string; online: boolean }
	| { op: 'things-changed' }
	| { error: string; thing?: string; property?: string };

const key = (thing: string, property: string) => `${thing}\n${property}`;

export class EriaClient {
	#listeners = new Map<string, Set<Listener>>();
	#values = new Map<string, unknown>();
	#handles = new Map<string, ThingHandle>();
	#ws?: WebSocket;
	#retry = 1000;

	/** Thing went on/offline. */
	onStatus: (thing: string, online: boolean) => void = () => {};
	/** Things appeared or vanished: re-read the services. */
	onThingsChanged: () => void = () => {};
	/** WebSocket connection state, for the "reconnecting" notice. */
	onConnection: (connected: boolean) => void = () => {};
	/** The session is gone (expired, signed out elsewhere, PIN changed). */
	onUnauthorized: () => void = () => {};

	constructor(
		private base = '',
		private socket = (url: string) => new WebSocket(url)
	) {}

	async services(): Promise<Service[]> {
		return this.#fetch('GET', '/api/services');
	}

	/** Who is signed in; throws HttpError 401 when nobody. */
	me(): Promise<Profile> {
		return this.#fetch('GET', '/api/me');
	}

	/** Names for the login screen (public). */
	signInProfiles(): Promise<Profile[]> {
		return this.#fetch('GET', '/api/auth/profiles');
	}

	async login(profile: number, pin: string) {
		await this.#fetch('POST', '/api/auth/login', { profile, pin });
	}

	async logout() {
		this.disconnect();
		await this.#fetch('POST', '/api/auth/logout');
	}

	/** Closes the shared WebSocket without reconnecting. */
	disconnect() {
		const ws = this.#ws;
		this.#ws = undefined;
		ws?.close();
	}

	/** Profile management (admin). */
	profiles(): Promise<Profile[]> {
		return this.#fetch('GET', '/api/profiles');
	}
	createProfile(p: { name: string; role: Role; pin: string }): Promise<Profile> {
		return this.#fetch('POST', '/api/profiles', p);
	}
	async updateProfile(id: number, changes: { name?: string; role?: Role; pin?: string }) {
		await this.#fetch('PUT', `/api/profiles/${id}`, changes);
	}
	async deleteProfile(id: number) {
		await this.#fetch('DELETE', `/api/profiles/${id}`);
	}

	/** Widget packs installed on the server (PACKS.md). */
	packs(): Promise<PackInfo[]> {
		return this.#fetch('GET', '/api/packs');
	}

	/** Own preferences of the signed-in profile. */
	async savePreferences(prefs: { pack?: string; theme?: string; scheme?: string }) {
		await this.#fetch('PUT', '/api/me', prefs);
	}

	/** Custom screens visible to the signed-in profile (shared first). */
	screens(): Promise<ScreenData[]> {
		return this.#fetch('GET', '/api/screens');
	}
	createScreen(title: string, shared: boolean): Promise<{ id: number }> {
		return this.#fetch('POST', '/api/screens', { title, shared });
	}
	/** Saves a whole draft; HttpError 409 when someone saved in between. */
	async saveScreen(s: ScreenData) {
		await this.#fetch('PUT', `/api/screens/${s.id}`, s);
	}
	async deleteScreen(id: number) {
		await this.#fetch('DELETE', `/api/screens/${id}`);
	}

	/** Contexts offered as modes (backend config). */
	async modes(): Promise<{ id: string; label: string }[]> {
		return this.#fetch('GET', '/api/modes');
	}

	/** Same handle for the same Thing, so widgets don't resubscribe on re-render. */
	thing(id: string): ThingHandle {
		let handle = this.#handles.get(id);
		if (!handle) {
			handle = this.#newHandle(id);
			this.#handles.set(id, handle);
		}
		return handle;
	}

	#newHandle(id: string): ThingHandle {
		const path = (kind: string, name: string) =>
			`/api/things/${encodeURIComponent(id)}/${kind}/${name.split('/').map(encodeURIComponent).join('/')}`;
		return {
			id,
			read: (p) => this.#fetch('GET', path('properties', p)),
			write: async (p, v) => {
				await this.#fetch('PUT', path('properties', p), v);
			},
			invoke: (a, input) => this.#fetch('POST', path('actions', a), input),
			observe: (p, l) => this.observe(id, p, l)
		};
	}

	observe(thing: string, property: string, listener: Listener): () => void {
		const k = key(thing, property);
		let set = this.#listeners.get(k);
		if (!set) {
			set = new Set();
			this.#listeners.set(k, set);
			this.#send({ op: 'observe', thing, property });
		}
		set.add(listener);
		if (this.#values.has(k)) listener(this.#values.get(k));
		return () => {
			set.delete(listener);
			if (set.size === 0 && this.#listeners.get(k) === set) {
				this.#listeners.delete(k);
				this.#values.delete(k);
				this.#send({ op: 'unobserve', thing, property });
			}
		};
	}

	async #fetch<T>(method: string, path: string, body?: unknown): Promise<T> {
		// Writes are always JSON: the backend refuses anything else (CSRF guard).
		const res = await fetch(this.base + path, {
			method,
			headers: method === 'GET' ? {} : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body)
		});
		const text = await res.text();
		if (!res.ok) {
			if (res.status === 401 && path !== '/api/auth/login') this.onUnauthorized();
			throw new HttpError(text.trim() || `HTTP ${res.status}`, res.status, Number(res.headers.get('Retry-After') ?? 0));
		}
		return (text ? JSON.parse(text) : undefined) as T;
	}

	#send(msg: object) {
		if (this.#ws?.readyState === WebSocket.OPEN) this.#ws.send(JSON.stringify(msg));
		else this.connect(); // observations are (re)sent on open
	}

	/** Opens the shared WebSocket (also needed for on/offline notices). */
	connect() {
		if (this.#ws) return;
		const url = new URL('/api/ws', this.base || location.href);
		url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
		const ws = this.socket(url.href);
		this.#ws = ws;
		ws.onopen = () => {
			this.#retry = 1000;
			this.onConnection(true);
			for (const k of this.#listeners.keys()) {
				const [thing, property] = k.split('\n');
				ws.send(JSON.stringify({ op: 'observe', thing, property }));
			}
		};
		ws.onmessage = (e) => this.#receive(JSON.parse(e.data) as ServerMsg);
		ws.onclose = () => {
			if (this.#ws !== ws) return; // closed on purpose (logout)
			this.#ws = undefined;
			this.onConnection(false);
			setTimeout(() => this.connect(), this.#retry);
			this.#retry = Math.min(this.#retry * 2, 30_000);
		};
	}

	#receive(msg: ServerMsg) {
		if ('value' in msg) {
			const k = key(msg.thing, msg.property);
			this.#values.set(k, msg.value);
			this.#listeners.get(k)?.forEach((l) => l(msg.value));
		} else if ('online' in msg) {
			this.onStatus(msg.thing, msg.online);
		} else if ('op' in msg) {
			this.onThingsChanged();
		} else {
			console.warn('[eria] server error', msg);
		}
	}
}

export const client = new EriaClient();
