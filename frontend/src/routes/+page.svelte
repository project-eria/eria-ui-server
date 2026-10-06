<script lang="ts">
	// Home: what one checks or changes most often.
	import Section from '#lib/Section.svelte';
	import { sections } from '#lib/sections.ts';
	import { m } from '#lib/paraglide/messages.js';
</script>

<h1>{m.nav_home()}</h1>
<Section section={sections.modes} />
<Section section={sections.climate} />

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem 1.25rem;
	}
</style>
