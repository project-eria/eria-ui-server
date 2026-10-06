<script lang="ts">
	// Own preferences: theme, color scheme and widget pack.
	// Admins also see every installed pack, with its state.
	import { client } from '#lib/client/eria.ts';
	import { app } from '#lib/state.svelte.ts';
	import { ensurePack } from '#lib/packs.ts';
	import { m } from '#lib/paraglide/messages.js';
	import { schemes, themes } from '#lib/themes.ts';

	let pack = $state(app.me?.pack || 'standard');
	let theme = $state(app.me?.theme || 'ardoise');
	let scheme = $state(app.me?.scheme || 'auto');
	let message = $state('');

	async function save(e: SubmitEvent) {
		e.preventDefault();
		try {
			const prefs = { pack: pack === 'standard' ? '' : pack, theme, scheme };
			await client.savePreferences(prefs);
			app.me = { ...app.me!, ...prefs };
			await ensurePack(pack);
			message = app.packErrors[pack] ? m.pack_failed({ detail: app.packErrors[pack] }) : m.preferences_saved();
		} catch (err) {
			message = m.edit_failed({ detail: err instanceof Error ? err.message : String(err) });
		}
	}
</script>

<h1>{m.nav_preferences()}</h1>

<form onsubmit={save}>
	<fieldset>
		<legend>{m.theme_choose()}</legend>
		{#each themes as t (t.id)}
			<label class="check"><input type="radio" name="theme" value={t.id} bind:group={theme} /> {t.name()}</label>
		{/each}
	</fieldset>
	<fieldset>
		<legend>{m.scheme_choose()}</legend>
		{#each schemes as sc (sc.id)}
			<label class="check"><input type="radio" name="scheme" value={sc.id} bind:group={scheme} /> {sc.name()}</label>
		{/each}
	</fieldset>
	<fieldset>
		<legend>{m.pack_choose()}</legend>
		<p class="help" id="pack-help">{m.pack_help()}</p>
		<label class="check">
			<input type="radio" name="pack" value="standard" bind:group={pack} aria-describedby="pack-help" />
			{m.pack_standard()}
		</label>
		{#each app.packs.filter((p) => !p.error) as p (p.id)}
			<label class="check">
				<input type="radio" name="pack" value={p.id} bind:group={pack} aria-describedby="pack-help" />
				{p.report ? p.name : m.pack_unverified({ name: p.name })}
			</label>
		{/each}
	</fieldset>
	<p class="status" role="status">{message}</p>
	<button type="submit">{m.profile_save()}</button>
</form>

{#if app.me?.role === 'admin'}
	<h2>{m.packs_installed()}</h2>
	{#if app.packs.length === 0}
		<p class="help">{m.packs_none()}</p>
	{:else}
		<ul class="packs">
			{#each app.packs as p (p.id)}
				<li>
					<span class="name">{p.name || p.id}</span>
					<span class="help">{p.id} · {p.version}</span>
					{#if p.error}
						<span class="error">{m.pack_invalid({ detail: p.error })}</span>
					{:else if app.packErrors[p.id]}
						<span class="error">{m.pack_failed({ detail: app.packErrors[p.id] })}</span>
					{:else}
						<span>{p.report ? m.pack_with_report() : m.pack_without_report()}</span>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
{/if}

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem 1rem;
	}
	h2 {
		font-size: 1.25rem;
		margin-block: 2rem 0.5rem;
	}
	form {
		display: grid;
		gap: 0.75rem;
		max-inline-size: 32rem;
	}
	fieldset {
		border: 0;
		margin: 0;
		padding: 0;
	}
	legend {
		font-weight: 600;
	}
	.help {
		color: var(--eria-muted);
		margin: 0.25rem 0 0.5rem;
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
		margin: 0;
	}
	.status {
		min-block-size: 1.5rem;
		margin: 0;
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
	.packs {
		display: grid;
		gap: 0.5rem;
		max-inline-size: 40rem;
		padding: 0;
		list-style: none;
	}
	.packs li {
		display: grid;
		gap: 0.125rem;
		padding: 0.625rem 0.875rem;
		border: 1px solid var(--eria-line);
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
	}
	.name {
		font-weight: 600;
	}
	.error {
		color: var(--eria-danger);
		font-weight: 600;
	}
</style>
