<script lang="ts">
	// Custom screens: shared ones, then one's own; and creating a new one.
	import { goto } from '$app/navigation';
	import { client } from '#lib/client/eria.ts';
	import { app, loadScreens } from '#lib/state.svelte.ts';
	import { m } from '#lib/paraglide/messages.js';

	let title = $state('');
	let shared = $state(false);
	let error = $state('');

	const groups = $derived([
		{ heading: m.screens_shared(), list: app.screens.filter((s) => s.shared) },
		{ heading: m.screens_mine(), list: app.screens.filter((s) => !s.shared) }
	]);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		try {
			const { id } = await client.createScreen(title, shared);
			await loadScreens();
			await goto(`/screens/${id}`);
		} catch (err) {
			error = m.edit_failed({ detail: err instanceof Error ? err.message : String(err) });
		}
	}
</script>

<h1>{m.nav_screens()}</h1>

{#each groups as group (group.heading)}
	<h2>{group.heading}</h2>
	{#if group.list.length === 0}
		<p class="muted">{m.screens_none()}</p>
	{:else}
		<ul>
			{#each group.list as s (s.id)}
				<li><a href="/screens/{s.id}">{s.title}</a></li>
			{/each}
		</ul>
	{/if}
{/each}

<h2>{m.screen_new()}</h2>
<form onsubmit={create}>
	<label>{m.screen_title()} <input bind:value={title} required autocomplete="off" /></label>
	{#if app.me?.role === 'admin'}
		<label class="check"><input type="checkbox" bind:checked={shared} /> {m.screen_shared()}</label>
	{/if}
	<p class="error" role="alert">{error}</p>
	<button type="submit">{m.screen_create()}</button>
</form>

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem 1rem;
	}
	h2 {
		font-size: 1.25rem;
		margin-block: 1.5rem 0.5rem;
	}
	ul {
		padding-inline-start: 1.25rem;
	}
	li {
		min-block-size: 2.75rem;
		display: list-item;
		line-height: 2.75rem;
	}
	a {
		color: var(--eria-ink);
		font-weight: 600;
	}
	.muted {
		color: var(--eria-muted);
	}
	form {
		display: grid;
		gap: 0.75rem;
		max-inline-size: 28rem;
	}
	label {
		display: grid;
		gap: 0.25rem;
		font-weight: 600;
	}
	label.check {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		min-block-size: 2.75rem;
	}
	label.check input {
		inline-size: 1.25rem;
		block-size: 1.25rem;
	}
	input:not([type='checkbox']) {
		min-block-size: 2.75rem;
		padding: 0.375rem 0.75rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-surface);
		color: var(--eria-ink);
		font: inherit;
		font-weight: 400;
	}
	.error {
		margin: 0;
		color: var(--eria-danger);
		font-weight: 600;
	}
	button {
		justify-self: start;
		min-block-size: 2.75rem;
		padding-inline: 1.25rem;
		border: 0;
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-ink);
		color: var(--eria-surface);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
</style>
