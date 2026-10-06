// Builds the widget-pack SDK as one ES module at a stable URL
// (backend/web/sdk/eria-widgets.js). Runs after the app build, which empties backend/web.
import { paraglideVitePlugin } from '@inlang/paraglide-js';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['preferredLanguage', 'baseLocale']
		})
	],
	build: {
		lib: { entry: 'src/sdk/index.ts', formats: ['es'], fileName: () => 'eria-widgets.js' },
		outDir: '../backend/web/sdk',
		emptyOutDir: true
	}
});
