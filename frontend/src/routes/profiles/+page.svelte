<script lang="ts">
	// Profile management (admin). Roles are provisional (PLAN §6).
	import { onMount } from 'svelte';
	import { client, type Profile, type Role } from '#lib/client/eria.ts';
	import { app } from '#lib/state.svelte.ts';
	import { m } from '#lib/paraglide/messages.js';

	const roles: { value: Role; label: () => string }[] = [
		{ value: 'admin', label: m.role_admin },
		{ value: 'member', label: m.role_member },
		{ value: 'guest', label: m.role_guest }
	];
	const roleName = (r?: Role) => roles.find((x) => x.value === r)?.label() ?? '';

	let profiles = $state<Profile[]>([]);
	let message = $state(''); // result of the last change, announced
	let draft = $state({ name: '', role: 'member' as Role, pin: '' });

	async function reload() {
		profiles = await client.profiles();
	}
	onMount(reload);

	// Runs a change, reloads, and says what happened.
	async function change(run: () => Promise<unknown>, done: string) {
		try {
			await run();
			message = done;
			await reload();
		} catch (e) {
			message = m.profile_failed({ detail: e instanceof Error ? e.message : String(e) });
		}
	}

	function add(e: SubmitEvent) {
		e.preventDefault();
		const name = draft.name;
		change(() => client.createProfile(draft), m.profile_created({ name })).then(() => {
			if (message === m.profile_created({ name })) draft = { name: '', role: 'member', pin: '' };
		});
	}

	function save(e: SubmitEvent, p: Profile) {
		e.preventDefault();
		const form = new FormData(e.target as HTMLFormElement);
		const pin = String(form.get('pin') ?? '');
		const changes = {
			name: String(form.get('name')),
			role: String(form.get('role')) as Role,
			...(pin ? { pin } : {})
		};
		change(() => client.updateProfile(p.id, changes), m.profile_saved({ name: changes.name }));
	}

	function remove(p: Profile) {
		if (confirm(m.profile_confirm_delete({ name: p.name }))) {
			change(() => client.deleteProfile(p.id), m.profile_deleted({ name: p.name }));
		}
	}
</script>

<h1>{m.nav_profiles()}</h1>

{#if app.me?.role !== 'admin'}
	<p>{m.profile_admin_only()}</p>
{:else}
	<p class="status" role="status">{message}</p>

	<ul class="list">
		{#each profiles as p (p.id)}
			<li>
				<details>
					<summary><span class="name">{p.name}</span> <span class="muted">{roleName(p.role)}</span></summary>
					<form onsubmit={(e) => save(e, p)}>
						<label>{m.profile_name()} <input name="name" value={p.name} required autocomplete="off" /></label>
						<label
							>{m.profile_role()}
							<select name="role" value={p.role}>
								{#each roles as r (r.value)}<option value={r.value}>{r.label()}</option>{/each}
							</select>
						</label>
						<label
							>{m.profile_new_pin()}
							<input name="pin" type="password" inputmode="numeric" pattern="[0-9]{'{'}4,8{'}'}" maxlength="8" autocomplete="new-password" />
						</label>
						<div class="buttons">
							<button type="submit" class="primary">{m.profile_save()}</button>
							<button type="button" onclick={() => remove(p)}>{m.profile_delete()}</button>
						</div>
					</form>
				</details>
			</li>
		{/each}
	</ul>

	<h2>{m.profile_add()}</h2>
	<form class="add" onsubmit={add}>
		<label>{m.profile_name()} <input bind:value={draft.name} required autocomplete="off" /></label>
		<label
			>{m.profile_role()}
			<select bind:value={draft.role}>
				{#each roles as r (r.value)}<option value={r.value}>{r.label()}</option>{/each}
			</select>
		</label>
		<label
			>{m.login_pin()}
			<input bind:value={draft.pin} type="password" inputmode="numeric" pattern="[0-9]{'{'}4,8{'}'}" maxlength="8" required autocomplete="new-password" />
		</label>
		<p class="muted">{m.login_pin_help()}</p>
		<button type="submit" class="primary">{m.profile_add()}</button>
	</form>
{/if}

<style>
	h1 {
		font-size: 1.75rem;
		margin-block: 0.5rem 1rem;
	}
	h2 {
		font-size: 1.375rem;
		margin-block: 2rem 0.75rem;
	}
	.status {
		min-block-size: 1.5rem;
		font-weight: 600;
	}
	.list {
		display: grid;
		gap: 0.5rem;
		max-inline-size: 36rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	details,
	.add {
		border: 1px solid var(--eria-line);
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
	}
	summary {
		display: flex;
		gap: 0.75rem;
		align-items: center;
		min-block-size: 3rem;
		padding-inline: 1rem;
		cursor: pointer;
	}
	.name {
		font-weight: 600;
	}
	.muted {
		color: var(--eria-muted);
	}
	form {
		display: grid;
		gap: 0.75rem;
		padding: 0 1rem 1rem;
	}
	.add {
		max-inline-size: 36rem;
		padding-block-start: 1rem;
	}
	label {
		display: grid;
		gap: 0.25rem;
		font-weight: 600;
	}
	input,
	select {
		min-block-size: 2.75rem;
		padding: 0.375rem 0.625rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-bg);
		color: var(--eria-ink);
		font: inherit;
		font-weight: 400;
	}
	.buttons {
		display: flex;
		gap: 0.5rem;
	}
	button {
		min-block-size: 2.75rem;
		padding-inline: 1rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-bg);
		color: var(--eria-ink);
		font: inherit;
		cursor: pointer;
	}
	.primary {
		background: var(--eria-ink);
		color: var(--eria-surface);
		border-color: transparent;
		justify-self: start;
	}
</style>
