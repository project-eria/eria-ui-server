<script lang="ts">
	// A predefined section: its services as cards, at their default size.
	import { app } from './state.svelte.ts';
	import { m } from './paraglide/messages.js';
	import type { Section } from './sections.ts';
	import { resolve, widgetFor, type PackInfo } from '../widgets';
	import { packFor } from './packs.ts';

	// pack: given by a custom screen; otherwise the profile's.
	let { section, heading = 'h2', pack }: { section: Section; heading?: 'h1' | 'h2'; pack?: PackInfo } = $props();
	const active = $derived(pack ?? packFor());

	const cards = $derived(
		app.services
			.filter((s) => section.capabilities.includes(s.capability))
			.map((s) => {
				const spec = widgetFor(s, section.variant, active)!;
				const [w, h] = resolve(s.capability, active)!.sizes[spec.props.variant as typeof section.variant]![0];
				return { spec, w, h };
			})
	);
</script>

<section aria-labelledby="section-{section.id}">
	<svelte:element this={heading} id="section-{section.id}">{section.title()}</svelte:element>
	{#if app.loading}
		<p>{m.loading()}</p>
	{:else if cards.length === 0}
		<p class="empty">{m.section_empty()}</p>
	{:else}
		<ul class="card-grid">
			{#each cards as { spec, w, h } (spec.key)}
				<li style:grid-column="span {w}" style:grid-row="span {h}">
					<svelte:element this={spec.tag} {...spec.props}></svelte:element>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	section + :global(section) {
		margin-block-start: 2rem;
	}
	h1,
	h2 {
		margin-block-end: 0.75rem;
	}
	h1 {
		font-size: 1.75rem;
	}
	h2 {
		font-size: 1.375rem;
	}
	.empty {
		color: var(--eria-muted);
	}
</style>
