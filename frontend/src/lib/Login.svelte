<script lang="ts">
	// Sign-in: pick your profile, type your PIN.
	import { onMount, tick } from 'svelte';
	import { client, HttpError, type Profile } from './client/eria.ts';
	import { start } from './state.svelte.ts';
	import { m } from './paraglide/messages.js';

	let profiles = $state<Profile[]>([]);
	let selected = $state<number>();
	let pin = $state('');
	let error = $state('');
	let busy = $state(false);
	let pinInput: HTMLInputElement;

	onMount(async () => {
		try {
			profiles = await client.signInProfiles();
			if (profiles.length === 1) selected = profiles[0].id;
		} catch (e) {
			error = m.load_error({ detail: e instanceof Error ? e.message : String(e) });
		}
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (selected === undefined) {
			error = m.login_choose_profile();
			return;
		}
		busy = true;
		error = '';
		try {
			await client.login(selected, pin);
			await start();
		} catch (err) {
			const locked = err instanceof HttpError && err.status === 429;
			error = locked
				? m.login_locked({ minutes: Math.max(1, Math.ceil(err.retryAfter / 60)) })
				: err instanceof HttpError && err.status === 401
					? m.login_wrong_pin()
					: m.load_error({ detail: String(err) });
			pin = '';
			await tick();
			pinInput.focus();
		} finally {
			busy = false;
		}
	}
</script>

<main id="content" class="login">
	<h1>{m.login_heading()}</h1>
	<form onsubmit={submit} novalidate>
		<fieldset>
			<legend>{m.login_profile()}</legend>
			<div class="profiles">
				{#each profiles as profile (profile.id)}
					<label class="profile">
						<input type="radio" name="profile" value={profile.id} bind:group={selected} />
						<span>{profile.name}</span>
					</label>
				{/each}
			</div>
		</fieldset>

		<label for="pin">{m.login_pin()}</label>
		<input
			id="pin"
			bind:this={pinInput}
			bind:value={pin}
			type="password"
			inputmode="numeric"
			autocomplete="current-password"
			pattern="[0-9]*"
			maxlength="8"
			required
			aria-describedby="pin-help pin-error"
			aria-invalid={error ? 'true' : undefined}
		/>
		<p id="pin-help" class="help">{m.login_pin_help()}</p>

		<!-- Always in the DOM so the message is announced when it appears. -->
		<p class="error" id="pin-error" role="alert">{error}</p>

		<button type="submit" disabled={busy}>{m.login_submit()}</button>
	</form>
</main>

<style>
	.login {
		max-inline-size: 32rem;
		margin-inline: auto;
		padding: 2rem 1rem;
	}
	h1 {
		font-size: 1.75rem;
		margin-block-end: 1.5rem;
	}
	fieldset {
		border: 0;
		margin: 0 0 1.5rem;
		padding: 0;
	}
	legend {
		font-weight: 600;
		margin-block-end: 0.5rem;
	}
	.profiles {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
		gap: 0.5rem;
	}
	/* The whole card is the radio's label: a big touch target. */
	.profile {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		min-block-size: 3.5rem;
		padding: 0.5rem 0.875rem;
		border: 1px solid var(--eria-line);
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
		font-weight: 600;
		cursor: pointer;
	}
	.profile:has(:checked) {
		border: 2px solid var(--eria-ink);
	}
	.profile input {
		inline-size: 1.25rem;
		block-size: 1.25rem;
		margin: 0;
		accent-color: var(--eria-ink);
	}
	label[for='pin'] {
		display: block;
		font-weight: 600;
		margin-block-end: 0.375rem;
	}
	#pin {
		box-sizing: border-box;
		inline-size: 100%;
		min-block-size: 3rem;
		padding: 0.5rem 0.75rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-surface);
		color: var(--eria-ink);
		font: inherit;
		font-size: 1.5rem;
		letter-spacing: 0.3em;
	}
	#pin[aria-invalid='true'] {
		border: 2px solid var(--eria-danger);
	}
	.help {
		margin: 0.375rem 0 0;
		color: var(--eria-muted);
	}
	.error {
		min-block-size: 1.5rem;
		margin: 1rem 0;
		color: var(--eria-danger);
		font-weight: 600;
	}
	button {
		min-block-size: 3rem;
		padding-inline: 1.5rem;
		border: 0;
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-ink);
		color: var(--eria-surface);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
</style>
