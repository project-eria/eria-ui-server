<script lang="ts">
	import '@fontsource-variable/atkinson-hyperlegible-next';
	import '../app.css';
	import '../widgets';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import Login from '#lib/Login.svelte';
	import { app, signOut, start } from '#lib/state.svelte.ts';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { applyTheme, pick } from '#lib/themes.ts';
	import type { WidgetErrorDetail } from '../widgets/base';

	let { children } = $props();

	const links = $derived([
		{ href: '/', label: m.nav_home },
		{ href: '/lights', label: m.nav_lights },
		{ href: '/openings', label: m.nav_openings },
		{ href: '/climate', label: m.nav_climate },
		{ href: '/modes', label: m.nav_modes },
		{ href: '/services', label: m.nav_services },
		// Custom screens: shared first, then one's own (already in that order).
		...app.screens.map((s) => ({ href: `/screens/${s.id}`, label: () => s.title })),
		{ href: '/screens', label: m.nav_screens },
		...(app.me?.role === 'admin' ? [{ href: '/profiles', label: m.nav_profiles }] : [])
	]);

	// Unique page title per screen (WCAG 2.4.2).
	const current = $derived(
		[...links, { href: '/preferences', label: m.nav_preferences }].find((l) => l.href === page.url.pathname)
	);

	// Theme of the page: the custom screen's, else the profile's (PLAN §5.3).
	$effect(() => {
		const id = page.url.pathname.match(/^\/screens\/(\d+)/)?.[1];
		const screen = id ? app.screens.find((s) => s.id === Number(id)) : undefined;
		applyTheme(pick(screen?.theme, app.me?.theme, 'ardoise'), pick(screen?.scheme, app.me?.scheme, 'auto'));
	});

	// Failed commands reported by widgets, shown until dismissed.
	let errors = $state<{ id: number; text: string }[]>([]);
	let nextId = 0;

	onMount(() => {
		document.documentElement.lang = getLocale();
		start();
		const onError = (e: Event) => {
			const { label, message } = (e as CustomEvent<WidgetErrorDetail>).detail;
			errors.push({ id: nextId++, text: m.command_failed({ label, detail: message }) });
		};
		document.addEventListener('eria-error', onError);
		return () => document.removeEventListener('eria-error', onError);
	});
</script>

<svelte:head>
	<title>
		{app.me === null ? `${m.login_heading()} – ${m.app_title()}` : current ? `${current.label()} – ${m.app_title()}` : m.app_title()}
	</title>
</svelte:head>

{#snippet who(name: string)}
	<span>{name}</span>
	<a href="/preferences" aria-current={page.url.pathname === '/preferences' ? 'page' : undefined}>{m.nav_preferences()}</a>
	<button type="button" onclick={signOut}>{m.sign_out()}</button>
{/snippet}

{#if app.me === null}
	<Login />
{:else if app.me}
<a class="skip" href="#content">{m.skip_to_content()}</a>

<div class="shell">
	<nav aria-label={m.nav_label()}>
		<ul>
			{#each links as link (link.href)}
				<li>
					<a href={link.href} aria-current={page.url.pathname === link.href ? 'page' : undefined}>
						{link.label()}
					</a>
				</li>
			{/each}
		</ul>
		<div class="who rail">{@render who(app.me.name)}</div>
	</nav>

	<main id="content" tabindex="-1">
		<div class="who phone">{@render who(app.me.name)}</div>
		<!-- Always in the DOM so screen readers pick up what gets added. -->
		<div class="notices" role="alert">
			{#each errors as error (error.id)}
				<p class="error">
					<span>{error.text}</span>
					<button type="button" onclick={() => (errors = errors.filter((x) => x.id !== error.id))}>
						{m.dismiss()}
					</button>
				</p>
			{/each}
		</div>
		<div role="status">
			{#if !app.connected}<p class="notice">{m.reconnecting()}</p>{/if}
			{#if app.me.pack && app.packErrors[app.me.pack]}
				<p class="notice">{m.pack_failed({ detail: app.packErrors[app.me.pack] })}</p>
			{/if}
		</div>

		{@render children()}
	</main>
</div>
{/if}

<style>
	.skip {
		position: absolute;
		inset-inline-start: 1rem;
		inset-block-start: -10rem;
		z-index: 10;
		padding: 0.75rem 1rem;
		background: var(--eria-surface);
		color: var(--eria-ink);
	}
	.skip:focus {
		inset-block-start: 1rem;
	}

	/* Phone: navigation as a bottom bar, in reach of the thumb. */
	.shell {
		min-block-size: 100dvh;
		padding-block-end: 4.5rem;
	}
	nav {
		position: fixed;
		inset: auto 0 0 0;
		z-index: 5;
		background: var(--eria-surface);
		border-block-start: 1px solid var(--eria-line);
		padding-block-end: env(safe-area-inset-bottom);
	}
	/* ponytail: text-only bar scrolls sideways on narrow phones; icons would let it fit. */
	nav ul {
		display: flex;
		justify-content: space-around;
		margin: 0;
		padding: 0;
		list-style: none;
		overflow-x: auto;
	}
	nav a {
		display: flex;
		align-items: center;
		justify-content: center;
		min-block-size: 3.5rem;
		min-inline-size: 2.75rem;
		padding-inline: 0.75rem;
		white-space: nowrap;
		color: var(--eria-muted);
		font-weight: 600;
		text-decoration: none;
	}
	.who {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 0.75rem;
		font-weight: 600;
	}
	.who.rail {
		display: none;
	}
	.who a {
		display: inline-flex;
		align-items: center;
		min-block-size: 2.75rem;
		color: var(--eria-ink);
	}
	.who button {
		min-block-size: 2.75rem;
		padding-inline: 0.75rem;
		border: 1px solid var(--eria-line);
		border-radius: calc(var(--eria-radius) - 0.25rem);
		background: var(--eria-bg);
		color: var(--eria-ink);
		font: inherit;
		cursor: pointer;
	}
	nav a[aria-current='page'] {
		color: var(--eria-ink);
		text-decoration: underline;
		text-decoration-thickness: 3px;
		text-underline-offset: 0.4em;
		/* Ink, not amber: amber only means "switched on". */
		text-decoration-color: currentColor;
	}

	/* Tablet and desktop: a side rail. */
	@media (min-width: 48rem) {
		.shell {
			display: grid;
			grid-template-columns: 11rem minmax(0, 1fr);
			padding-block-end: 0;
		}
		nav {
			position: sticky;
			inset: 0 auto auto 0;
			block-size: 100dvh;
			/* Many screens or 200 % text: the rail scrolls instead of hiding links. */
			overflow-y: auto;
			border-block-start: 0;
			border-inline-end: 1px solid var(--eria-line);
			padding-block: 1rem;
		}
		nav ul {
			flex-direction: column;
		}
		nav {
			display: flex;
			flex-direction: column;
			justify-content: space-between;
		}
		.who.phone {
			display: none;
		}
		.who.rail {
			display: flex;
			flex-direction: column;
			align-items: flex-start;
			gap: 0.5rem;
			padding: 1rem 0.75rem;
		}
		nav a {
			justify-content: flex-start;
			/* Long screen titles wrap in the rail (no sideways scrolling). */
			white-space: normal;
			overflow-wrap: anywhere;
			padding-block: 0.5rem;
		}
	}

	main {
		padding: 1rem;
		max-inline-size: 90rem;
	}
	.error,
	.notice {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		margin: 0 0 1rem;
		padding: 0.75rem 1rem;
		border-radius: var(--eria-radius);
		background: var(--eria-surface);
		border: 2px solid var(--eria-line);
	}
	.error {
		border-color: var(--eria-danger);
	}
	.error button {
		min-block-size: 2.75rem;
		padding-inline: 1rem;
		font: inherit;
	}
</style>
