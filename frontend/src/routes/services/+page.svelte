<script lang="ts">
	import { app } from '#lib/state.svelte.ts';
	import { m } from '#lib/paraglide/messages.js';
	import { widgetsFor } from '../../widgets';
	import { packFor } from '#lib/packs.ts';

	const capabilityName = (cap: string) => (m as unknown as Record<string, () => string>)[`cap_${cap}`]?.() ?? cap;
</script>

<h1>{m.services_heading()}</h1>

<!-- Live regions always present, so what appears in them is announced. -->
<div role="status">{#if app.loading}<p>{m.loading()}</p>{/if}</div>
<div role="alert">{#if !app.loading && app.error}<p>{m.load_error({ detail: app.error })}</p>{/if}</div>
{#if app.loading || app.error}{:else if app.services.length === 0}
	<p>{m.empty()}</p>
{:else}
	<ul class="services">
		{#each app.services as service (service.id)}
			<li>
				<section aria-labelledby="h-{service.id} m-{service.id}" class:offline={!service.online}>
					<h2 id="h-{service.id}">{service.label}</h2>
					<p class="meta" id="m-{service.id}">
						{capabilityName(service.capability)}
						{#if !service.online}<strong class="badge">{m.offline()}</strong>{/if}
					</p>
					<div class="widgets">
						{#each widgetsFor(service, packFor()) as w (w.key)}
							<svelte:element this={w.tag} {...w.props}></svelte:element>
						{/each}
					</div>
				</section>
			</li>
		{/each}
	</ul>
{/if}

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem 1.25rem;
	}
	.services {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 17rem), 1fr));
		gap: 1rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	h2 {
		font-size: 1.125rem;
	}
	.meta {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		margin: 0;
		color: var(--eria-muted);
	}
	.badge {
		padding: 0 0.5rem;
		border: 1px solid currentColor;
		border-radius: 1rem;
		font-weight: 600;
	}
	.widgets {
		display: grid;
		gap: 0.5rem;
	}
</style>
