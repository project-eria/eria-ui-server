import { expect, test, vi } from 'vitest';
import { EriaClient } from './eria';

// Minimal stand-in for the browser WebSocket.
class FakeSocket {
	readyState = 0;
	sent: unknown[] = [];
	onopen?: () => void;
	onmessage?: (e: { data: string }) => void;
	onclose?: () => void;
	send(data: string) {
		this.sent.push(JSON.parse(data));
	}
	open() {
		this.readyState = WebSocket.OPEN;
		this.onopen?.();
	}
	receive(msg: object) {
		this.onmessage?.({ data: JSON.stringify(msg) });
	}
}

test('observations are shared, cached and re-sent after a reconnection', () => {
	vi.useFakeTimers();
	const sockets: FakeSocket[] = [];
	const client = new EriaClient('http://h', () => {
		const s = new FakeSocket();
		sockets.push(s);
		return s as unknown as WebSocket;
	});

	const a: unknown[] = [];
	const b: unknown[] = [];
	const stopA = client.observe('urn:l', 'on', (v) => a.push(v));
	const stopB = client.observe('urn:l', 'on', (v) => b.push(v));
	const ws = sockets[0];
	ws.open();
	expect(ws.sent).toEqual([{ op: 'observe', thing: 'urn:l', property: 'on' }]); // once for 2 widgets

	ws.receive({ thing: 'urn:l', property: 'on', value: true });
	const late: unknown[] = [];
	const stopLate = client.observe('urn:l', 'on', (v) => late.push(v));
	expect([a, b, late]).toEqual([[true], [true], [true]]); // late widget gets the cached value

	stopA();
	stopLate();
	expect(ws.sent).toHaveLength(1); // B still watches
	stopB();
	expect(ws.sent.at(-1)).toEqual({ op: 'unobserve', thing: 'urn:l', property: 'on' });

	// Connection lost: reconnect and observe again what is still watched.
	client.observe('urn:t', 'temp', () => {});
	ws.onclose?.();
	vi.advanceTimersByTime(1000);
	sockets[1].open();
	expect(sockets[1].sent).toEqual([{ op: 'observe', thing: 'urn:t', property: 'temp' }]);
	vi.useRealTimers();
});

test('thing handles are stable and encode ids and names', async () => {
	const client = new EriaClient('http://h', () => new FakeSocket() as unknown as WebSocket);
	expect(client.thing('urn:a')).toBe(client.thing('urn:a'));

	const fetch = vi.fn(async () => new Response('"2026-10-05T19:26:45+02:00"'));
	vi.stubGlobal('fetch', fetch);
	await client.thing('urn:eria:service:astral:1').read('today/sunset');
	expect(fetch).toHaveBeenCalledWith(
		'http://h/api/things/urn%3Aeria%3Aservice%3Aastral%3A1/properties/today/sunset',
		expect.objectContaining({ method: 'GET' })
	);

	fetch.mockResolvedValueOnce(new Response('read-only property', { status: 403 }));
	await expect(client.thing('urn:a').write('liters', 3)).rejects.toThrow('read-only property');
	vi.unstubAllGlobals();
});
