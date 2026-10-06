import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import { defineConfig } from 'vitest/config';

// Backend used by `pnpm dev` (the test bench by default).
const backend = process.env.ERIA_BACKEND ?? 'http://10.0.0.215:8090';

export default defineConfig({
	plugins: [
		// SPA build, embedded and served by the Go backend (../backend/web).
		sveltekit({ adapter: adapter({ pages: '../backend/web', assets: '../backend/web', fallback: 'index.html' }) }),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['preferredLanguage', 'baseLocale']
		})
	],
	server: {
		// rewriteWsOrigin: the backend rejects cross-origin WebSockets
		proxy: {
			'/api': { target: backend, ws: true, rewriteWsOrigin: true },
			// Widget packs and their SDK are served by the backend
			'/packs': backend,
			'/sdk': backend
		}
	},
	test: { include: ['src/**/*.test.ts'] }
});
