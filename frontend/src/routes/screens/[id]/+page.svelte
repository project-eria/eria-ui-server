<script lang="ts">
	// A custom screen: a grid of cards, and its edit mode (PLAN §5, §5.1).
	// Editing works on a draft saved in one go; widgets are inert meanwhile.
	import { tick } from 'svelte';
	import { page } from '$app/state';
	import { beforeNavigate, goto } from '$app/navigation';
	import Section from '#lib/Section.svelte';
	import { client, HttpError, type ScreenData, type Service } from '#lib/client/eria.ts';
	import { app, loadScreens } from '#lib/state.svelte.ts';
	import { sections } from '#lib/sections.ts';
	import {
		actionable,
		cardName,
		cardSpec,
		multiReading,
		newId,
		serviceCard,
		variantsOf,
		type Card,
		type SectionId
	} from '#lib/cards.ts';
	import { m } from '#lib/paraglide/messages.js';
	import { ensurePack, packFor } from '#lib/packs.ts';
	import { schemes, themes } from '#lib/themes.ts';
	import type { Variant } from '../../../widgets/base';
	import { propertyName } from '../../../widgets/capabilities';

	const screen = $derived(app.screens.find((s) => s.id === Number(page.params.id)));
	let draft = $state<ScreenData | null>(null); // edit mode when set
	const editing = $derived(draft !== null);
	const shown = $derived(draft ?? screen);
	const cards = $derived((shown?.layout ?? []) as Card[]);
	const dirty = $derived(draft !== null && JSON.stringify(draft) !== JSON.stringify(screen));
	// Widget pack: the screen's own, else the profile's (Standard until loaded).
	const pack = $derived(packFor(shown?.pack));
	$effect(() => {
		ensurePack(shown?.pack || app.me?.pack);
	});

	let announce = $state(''); // polite live region
	let error = $state('');
	let editButton = $state<HTMLButtonElement>();
	let editHeading = $state<HTMLHeadingElement>();

	const variantName: Record<Variant, () => string> = { tile: m.variant_tile, full: m.variant_full, value: m.variant_value };
	const capabilityName = (cap: string) => (m as unknown as Record<string, () => string>)[`cap_${cap}`]?.() ?? cap;
	const serviceOf = (card: Card) => (card.kind === 'service' ? app.services.find((s) => s.id === card.service) : undefined);
	const name = (card: Card) => cardName(card, app.services);
	const layout = () => draft!.layout as Card[];

	// --- edit mode ---

	async function startEdit() {
		draft = structuredClone($state.snapshot(screen!)) as ScreenData;
		error = '';
		announce = m.edit_started();
		await tick();
		editHeading?.focus();
	}

	async function stopEdit(message: string) {
		draft = null;
		error = '';
		announce = message;
		await tick();
		editButton?.focus();
	}

	async function save() {
		if (!draft!.title.trim()) {
			error = m.edit_title_required();
			return;
		}
		try {
			await client.saveScreen($state.snapshot(draft!) as ScreenData);
			await loadScreens();
			await stopEdit(m.edit_saved());
		} catch (e) {
			error =
				e instanceof HttpError && e.status === 409
					? m.edit_conflict()
					: m.edit_failed({ detail: e instanceof Error ? e.message : String(e) });
		}
	}

	async function reloadAfterConflict() {
		await loadScreens();
		await stopEdit(m.edit_reloaded());
	}

	async function deleteScreen() {
		if (!confirm(m.screen_confirm_delete({ title: screen!.title }))) return;
		try {
			await client.deleteScreen(screen!.id);
			draft = null;
			await loadScreens();
			await goto('/screens');
		} catch (e) {
			error = m.edit_failed({ detail: e instanceof Error ? e.message : String(e) });
		}
	}

	// Moving keeps the focus on the button used, so it can be pressed again.
	async function move(i: number, delta: -1 | 1) {
		const list = layout();
		const j = i + delta;
		if (j < 0 || j >= list.length) return;
		[list[i], list[j]] = [list[j], list[i]];
		announce = m.card_moved({ name: name(list[j]), position: j + 1, total: list.length });
		await tick();
		document.getElementById(`${delta < 0 ? 'up' : 'down'}-${list[j].id}`)?.focus();
	}

	async function remove(i: number) {
		const list = layout();
		const [card] = list.splice(i, 1);
		announce = m.card_removed({ name: name(card) });
		await tick();
		const next = list[Math.min(i, list.length - 1)];
		(next ? document.getElementById(`set-${next.id}`) : document.getElementById('add-card'))?.focus();
	}

	beforeNavigate((nav) => {
		if (!dirty) return;
		if (nav.willUnload) nav.cancel(); // the browser asks itself
		else if (!confirm(m.edit_leave())) nav.cancel();
	});

	// --- card dialog: settings of a card, or a new card ---

	let dialog = $state<HTMLDialogElement>();
	let target = $state<number | null>(null); // index edited; null = new card
	let form = $state<Card | null>(null);
	let newKind = $state<Card['kind']>('service');
	let newService = $state('');
	let newSection = $state<SectionId>('lights');
	let newTitle = $state('');

	// showModal() after Svelte swapped the dialog content, or the element it
	// focused would be replaced and the focus lost.
	async function openSettings(i: number) {
		target = i;
		form = structuredClone($state.snapshot(layout()[i])) as Card;
		await tick();
		dialog!.showModal();
	}

	// Esc or Cancel with unsaved settings: ask before losing them.
	const dialogDirty = () =>
		target !== null
			? JSON.stringify($state.snapshot(form)) !== JSON.stringify($state.snapshot(layout()[target]))
			: newTitle.trim() !== '' || newService !== '';
	function closeDialog(e?: Event) {
		if (dialogDirty() && !confirm(m.card_discard())) {
			e?.preventDefault(); // keeps the dialog open (Esc)
			return;
		}
		dialog!.close();
	}

	async function openAdd() {
		target = null;
		form = null;
		newKind = 'service';
		newService = '';
		newTitle = '';
		await tick();
		dialog!.showModal();
	}

	async function applyDialog(e: SubmitEvent) {
		e.preventDefault();
		let card: Card;
		if (target !== null) {
			card = form!;
			layout()[target] = card;
			announce = m.card_updated({ name: name(card) });
		} else {
			const service = app.services.find((s) => s.id === newService);
			if (newKind === 'service' && !service) return;
			if (newKind === 'title' && !newTitle.trim()) return;
			card =
				newKind === 'service'
					? serviceCard(service!, pack)
					: newKind === 'section'
						? { id: newId(), kind: 'section', section: newSection }
						: { id: newId(), kind: 'title', text: newTitle.trim() };
			layout().push(card);
			announce = m.card_added({ name: name(card) });
		}
		dialog!.close();
		await tick();
		document.getElementById(`set-${card.id}`)?.focus();
	}

	// Services grouped by capability for the "add" list.
	const byCapability = $derived(
		Object.entries(
			app.services
				.filter((s) => s.capability !== 'generic')
				.reduce<Record<string, Service[]>>((groups, s) => {
					(groups[s.capability] ??= []).push(s);
					return groups;
				}, {})
		)
	);

	function setVariant(variant: Variant) {
		if (form?.kind !== 'service') return;
		const [w, h] = variantsOf(serviceOf(form)!, pack)[variant]![0];
		form = { ...form, variant, w, h };
	}
</script>

{#if !screen}
	<h1>{m.screen_not_found()}</h1>
	<p><a href="/screens">{m.nav_screens()}</a></p>
{:else}
	<div class="head">
		<h1>{shown!.title}</h1>
		{#if screen.canEdit && !editing}
			<button bind:this={editButton} type="button" onclick={startEdit}>{m.edit_page()}</button>
		{/if}
	</div>

	<p class="vh" role="status">{announce}</p>

	{#if editing && draft}
		<section class="editbar" aria-labelledby="edit-heading">
			<h2 id="edit-heading" tabindex="-1" bind:this={editHeading}>{m.edit_heading()}</h2>
			<div class="fields">
				<label>{m.screen_title()} <input bind:value={draft.title} required autocomplete="off" /></label>
				{#if draft.shared && app.me?.role === 'admin'}
					<label
						>{m.screen_edit_role()}
						<select bind:value={draft.editRole}>
							<option value="admin">{m.screen_edit_admins()}</option>
							<option value="member">{m.screen_edit_members()}</option>
						</select>
					</label>
				{/if}
				<label>{m.screen_position()} <input type="number" min="1" bind:value={draft.position} /></label>
				<label
					>{m.screen_theme()}
					<select bind:value={draft.theme}>
						<option value="">{m.pack_of_profile()}</option>
						{#each themes as t (t.id)}<option value={t.id}>{t.name()}</option>{/each}
					</select>
				</label>
				<label
					>{m.screen_scheme()}
					<select bind:value={draft.scheme}>
						<option value="">{m.pack_of_profile()}</option>
						{#each schemes as sc (sc.id)}<option value={sc.id}>{sc.name()}</option>{/each}
					</select>
				</label>
				<label
					>{m.screen_pack()}
					<select bind:value={draft.pack}>
						<option value="">{m.pack_of_profile()}</option>
						<option value="standard">{m.pack_standard()}</option>
						{#each app.packs.filter((p) => !p.error) as p (p.id)}
							<option value={p.id}>{p.report ? p.name : m.pack_unverified({ name: p.name })}</option>
						{/each}
					</select>
				</label>
			</div>
			<div class="buttons">
				<button id="add-card" type="button" onclick={openAdd}>{m.card_add()}</button>
				<button type="button" class="primary" onclick={save}>{m.edit_save()}</button>
				<button type="button" onclick={() => stopEdit(m.edit_cancelled())}>{m.edit_cancel()}</button>
				{#if !screen.shared || app.me?.role === 'admin'}
					<button type="button" class="danger" onclick={deleteScreen}>{m.screen_delete()}</button>
				{/if}
			</div>
			<div role="alert">
				{#if error}
					<p class="error">
						{error}
						{#if error === m.edit_conflict()}
							<button type="button" onclick={reloadAfterConflict}>{m.edit_reload()}</button>
						{/if}
					</p>
				{/if}
			</div>
		</section>
	{/if}

	{#if cards.length === 0}
		<p class="empty">{screen.canEdit ? m.screen_empty_editable() : m.screen_empty()}</p>
	{/if}

	<ul class="card-grid">
		{#each cards as card, i (card.id)}
			{@const service = serviceOf(card)}
			{@const spec = card.kind === 'service' && service ? cardSpec(card, service, pack) : undefined}
			<li
				class:wide={card.kind !== 'service'}
				class:editing
				style:grid-column={card.kind === 'service' ? `span ${card.w}` : undefined}
				style:grid-row={card.kind === 'service' ? `span ${card.h}` : undefined}
			>
				<div class="cell" inert={editing}>
					{#if card.kind === 'title'}
						<h2 class="title-card">{card.text}</h2>
					{:else if card.kind === 'section'}
						<Section section={sections[card.section]} {pack} />
					{:else if spec}
						<svelte:element this={spec.tag} {...spec.props}></svelte:element>
					{:else}
						<div class="unavailable">
							<span class="label">{name(card)}</span>
							<span>{m.card_unavailable()}</span>
						</div>
					{/if}
				</div>
				{#if editing}
					<div class="toolbar">
						<button id="set-{card.id}" type="button" onclick={() => openSettings(i)}
							>{m.card_settings()}<span class="vh"> {name(card)}</span></button
						>
						<button
							id="up-{card.id}"
							type="button"
							aria-disabled={i === 0}
							onclick={() => move(i, -1)}>{m.card_up()}<span class="vh"> {name(card)}</span></button
						>
						<button
							id="down-{card.id}"
							type="button"
							aria-disabled={i === cards.length - 1}
							onclick={() => move(i, 1)}>{m.card_down()}<span class="vh"> {name(card)}</span></button
						>
						<button type="button" onclick={() => remove(i)}>{m.card_remove()}<span class="vh"> {name(card)}</span></button>
					</div>
				{/if}
			</li>
		{/each}
	</ul>

	<!-- Esc handled on keydown: a second `cancel` in a row is not cancelable in Chromium. -->
	<dialog
		bind:this={dialog}
		aria-labelledby="card-dialog-title"
		oncancel={closeDialog}
		onkeydown={(e) => {
			if (e.key === 'Escape') {
				e.preventDefault();
				closeDialog();
			}
		}}
	>
		<form onsubmit={applyDialog}>
			{#if target !== null && form}
				<h2 id="card-dialog-title">{m.card_settings_of({ name: name(form) })}</h2>
				{#if form.kind === 'title'}
					<label>{m.card_title_text()} <input bind:value={form.text} required autocomplete="off" /></label>
				{:else if form.kind === 'section'}
					<label
						>{m.card_section()}
						<select bind:value={form.section}>
							{#each Object.values(sections) as s (s.id)}<option value={s.id}>{s.title()}</option>{/each}
						</select>
					</label>
				{:else}
					{@const service = serviceOf(form)}
					{#if service}
						{@const variants = variantsOf(service, pack)}
						<label
							>{m.card_variant()}
							<select value={form.variant} onchange={(e) => setVariant(e.currentTarget.value as Variant)}>
								{#each Object.keys(variants) as v (v)}<option value={v}>{variantName[v as Variant]()}</option>{/each}
							</select>
						</label>
						<label
							>{m.card_size()}
							<select
								value={`${form.w}x${form.h}`}
								onchange={(e) => {
									const [w, h] = e.currentTarget.value.split('x').map(Number);
									if (form?.kind === 'service') form = { ...form, w, h };
								}}
							>
								{#each variants[form.variant] ?? [] as [w, h] (`${w}x${h}`)}
									<option value={`${w}x${h}`}>{m.card_size_value({ w, h })}</option>
								{/each}
							</select>
						</label>
						<label
							>{m.card_label()}
							<input bind:value={form.label} placeholder={service.label} autocomplete="off" aria-describedby="label-help" />
						</label>
						<p id="label-help" class="help">{m.card_label_help()}</p>
						{#if multiReading(service)}
							<fieldset>
								<legend>{m.card_props()}</legend>
								{#each Object.keys(service.properties) as p (p)}
									<label class="check">
										<input
											type="checkbox"
											checked={!form.props?.length || form.props.includes(p)}
											onchange={(e) => {
												if (form?.kind !== 'service') return;
												const all = Object.keys(service.properties);
												const current = form.props?.length ? form.props : all;
												const next = e.currentTarget.checked ? [...current, p] : current.filter((x) => x !== p);
												form = { ...form, props: all.filter((x) => next.includes(x)) };
											}}
										/>
										{propertyName(p)}
									</label>
								{/each}
							</fieldset>
						{/if}
						{#if actionable.has(service.capability)}
							<label class="check">
								<input
									type="checkbox"
									checked={form.confirm ?? service.capability === 'GarageDoor'}
									onchange={(e) => form?.kind === 'service' && (form = { ...form, confirm: e.currentTarget.checked })}
								/>
								{m.card_confirm()}
							</label>
						{/if}
					{:else}
						<p>{m.card_unavailable()}</p>
					{/if}
				{/if}
				<div class="buttons">
					<button type="submit" class="primary">{m.card_apply()}</button>
					<button type="button" onclick={() => closeDialog()}>{m.edit_cancel()}</button>
				</div>
			{:else}
				<h2 id="card-dialog-title">{m.card_add()}</h2>
				<fieldset>
					<legend>{m.card_kind()}</legend>
					<label class="check"><input type="radio" bind:group={newKind} value="service" /> {m.card_kind_service()}</label>
					<label class="check"><input type="radio" bind:group={newKind} value="section" /> {m.card_kind_section()}</label>
					<label class="check"><input type="radio" bind:group={newKind} value="title" /> {m.card_kind_title()}</label>
				</fieldset>
				{#if newKind === 'service'}
					<label
						>{m.card_service()}
						<select bind:value={newService} required>
							<option value="" disabled>{m.card_choose()}</option>
							{#each byCapability as [cap, list] (cap)}
								<optgroup label={capabilityName(cap)}>
									{#each list as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
								</optgroup>
							{/each}
						</select>
					</label>
				{:else if newKind === 'section'}
					<label
						>{m.card_section()}
						<select bind:value={newSection}>
							{#each Object.values(sections) as s (s.id)}<option value={s.id}>{s.title()}</option>{/each}
						</select>
					</label>
				{:else}
					<label>{m.card_title_text()} <input bind:value={newTitle} required autocomplete="off" /></label>
				{/if}
				<div class="buttons">
					<button type="submit" class="primary">{m.card_add()}</button>
					<button type="button" onclick={() => closeDialog()}>{m.edit_cancel()}</button>
				</div>
			{/if}
		</form>
	</dialog>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		margin-block: 0.5rem 1.25rem;
	}
	h1 {
		font-size: 1.75rem;
		margin: 0;
	}
	button,
	input,
	select {
		min-block-size: 2.75rem;
		padding: 0.375rem 0.75rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-surface);
		color: var(--eria-ink);
		font: inherit;
	}
	button {
		cursor: pointer;
	}
	button[aria-disabled='true'] {
		cursor: not-allowed;
		color: var(--eria-muted);
	}
	.primary {
		background: var(--eria-ink);
		color: var(--eria-surface);
		border-color: transparent;
	}
	.danger {
		border-color: var(--eria-danger);
		color: var(--eria-danger);
	}
	.editbar {
		margin-block-end: 1.25rem;
		padding: 1rem;
		border: 2px dashed var(--eria-line);
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
	}
	.editbar h2 {
		font-size: 1.125rem;
		margin-block-end: 0.75rem;
	}
	.fields,
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
		align-items: end;
	}
	.buttons {
		margin-block-start: 0.75rem;
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
		min-block-size: auto;
		inline-size: 1.25rem;
		block-size: 1.25rem;
	}
	input,
	select {
		font-weight: 400;
	}
	.error {
		color: var(--eria-danger);
		font-weight: 600;
	}
	.empty,
	.help {
		color: var(--eria-muted);
	}
	.help {
		margin: 0;
	}
	li {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}
	.cell {
		flex: 1;
		min-block-size: 0;
	}
	.cell > :global(*) {
		block-size: 100%;
	}
	li.editing .cell {
		outline: 2px dashed var(--eria-line);
		outline-offset: 2px;
		border-radius: var(--eria-radius);
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem;
	}
	.toolbar button {
		flex: 1 1 auto;
		padding-inline: 0.5rem;
		font-size: 0.875rem;
	}
	.title-card {
		font-size: 1.375rem;
		margin-block: 1rem 0;
	}
	.unavailable {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		padding: 0.625rem 0.875rem;
		border: 1px dashed var(--eria-line);
		border-radius: var(--eria-radius);
		color: var(--eria-muted);
	}
	.unavailable .label {
		font-weight: 600;
		color: var(--eria-ink);
	}
	dialog {
		inline-size: min(30rem, calc(100vw - 2rem));
		padding: 1.25rem;
		border: 1px solid var(--eria-line);
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
		color: var(--eria-ink);
	}
	dialog::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	dialog form {
		display: grid;
		gap: 0.75rem;
	}
	dialog h2 {
		font-size: 1.25rem;
	}
	fieldset {
		border: 0;
		margin: 0;
		padding: 0;
	}
	legend {
		font-weight: 600;
		margin-block-end: 0.25rem;
	}
</style>
