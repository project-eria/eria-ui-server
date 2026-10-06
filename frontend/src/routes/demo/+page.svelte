<script lang="ts">
	// Widget showcase: every capability widget, in each variant and size, fed
	// by the first real service of that capability. Controls are live.
	import { app } from '#lib/state.svelte.ts';
	import { m } from '#lib/paraglide/messages.js';
	import { catalog, widgetFor } from '../../widgets';
	// Showcase of the Standard pack only.
	import type { Variant } from '../../widgets/base';

	const variantName: Record<Variant, () => string> = {
		tile: m.variant_tile,
		full: m.variant_full,
		value: m.variant_value
	};
	const capabilityName = (cap: string) => (m as unknown as Record<string, () => string>)[`cap_${cap}`]?.() ?? cap;

	const examples = $derived(
		Object.keys(catalog)
			.map((cap) => app.services.find((s) => s.capability === cap))
			.filter((s) => s !== undefined)
	);
</script>

<h1>{m.demo_heading()}</h1>
<p class="intro">{m.demo_intro()}</p>

{#each examples as service (service.id)}
	<section aria-labelledby="d-{service.id}">
		<h2 id="d-{service.id}">{capabilityName(service.capability)}</h2>
		{#each Object.entries(catalog[service.capability].sizes) as [variant, sizes] (variant)}
			<h3>{variantName[variant as Variant]()}</h3>
			<div class="sizes">
				{#each sizes as [w, h] (`${w}x${h}`)}
					{@const spec = widgetFor(service, variant as Variant)!}
					<figure>
						<figcaption>{w} × {h}</figcaption>
						<div class="grid">
							<div class="cell" style:grid-column="span {w}" style:grid-row="span {h}">
								<svelte:element this={spec.tag} {...spec.props}></svelte:element>
							</div>
						</div>
					</figure>
				{/each}
			</div>
		{/each}
	</section>
{/each}

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem;
	}
	.intro {
		color: var(--eria-muted);
		max-inline-size: 65ch;
	}
	section {
		margin-block: 2rem;
	}
	h2 {
		font-size: 1.375rem;
		margin-block-end: 0.5rem;
	}
	h3 {
		font-size: 1rem;
		margin-block: 1rem 0.5rem;
		color: var(--eria-muted);
	}
	.sizes {
		display: flex;
		flex-wrap: wrap;
		gap: 1.5rem;
		align-items: start;
	}
	figure {
		margin: 0;
	}
	figcaption {
		font-variant-numeric: tabular-nums;
		color: var(--eria-muted);
		margin-block-end: 0.25rem;
	}
	/* Same cell size as the future custom screens (PLAN §5.1). */
	.grid {
		display: grid;
		grid-template-columns: repeat(4, 5rem);
		grid-auto-rows: 5rem;
		gap: 0.5rem;
		max-inline-size: 100%;
	}
	.cell {
		min-inline-size: 0;
	}
	.cell > :global(*) {
		block-size: 100%;
	}
</style>
