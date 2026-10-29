<script lang="ts">
	import type { Api } from './lib/api';
	import type { LinkConfig } from './lib/links';
	import { load } from './lib/load.svelte';
	import type { Router } from './lib/router.svelte';
	import type { Route } from './lib/routes';
	import EntryPage from './pages/EntryPage.svelte';
	import NotFoundPage from './pages/NotFoundPage.svelte';
	import OpenPage from './pages/OpenPage.svelte';
	import OverviewPage from './pages/OverviewPage.svelte';
	import StoryPage from './pages/StoryPage.svelte';

	let { api, router }: { api: Api; router: Router } = $props();

	const route = $derived(router.route);
	let main: HTMLElement | undefined = $state();
	// The story and overview pages draw graphs, which take the whole width;
	// the others are text, read best in a narrow column.
	const narrow = $derived(route.name !== 'task' && route.name !== 'overview');

	// Loaded once: what the configuration says does not change while the
	// app runs. Until it is loaded, or when it fails, runs have no link.
	const config = load(() => (signal) => api.config(signal));
	const links: LinkConfig = $derived(config.current.state === 'loaded' ? config.current.value : {});

	$effect(() => router.start());

	// The page the route showed when the effect below last ran: plain, since
	// only that effect reads and writes it.
	let shown: Route['name'] | undefined;

	// After a navigation, move focus to the new page's heading, so that a
	// keyboard or screen reader user lands on what replaced the link. Moving
	// from one search to another keeps the entry page, and from one overview
	// filter to another the overview, with the focus where the reader is.
	$effect(() => {
		const samePage =
			(shown === 'home' && route.name === 'home') ||
			(shown === 'overview' && route.name === 'overview');
		shown = route.name;
		if (router.navigations > 0 && !samePage) main?.querySelector<HTMLElement>('h1')?.focus();
	});
</script>

<svelte:document onclick={(event) => router.handleClick(event)} />

{#if route.name !== 'home'}
	<header class:narrow><a href="/">Sillage</a></header>
{/if}
<main bind:this={main} class:narrow>
	{#if route.name === 'home'}
		<EntryPage {api} {router} query={route.query} />
	{:else if route.name === 'task'}
		<StoryPage {api} {links} root={{ task: route.key }} />
	{:else if route.name === 'open'}
		<OpenPage {api} {router} url={route.url} />
	{:else if route.name === 'overview'}
		<OverviewPage {api} {router} query={route.query} />
	{:else}
		<NotFoundPage path={route.path} />
	{/if}
</main>

<style>
	.narrow {
		margin: 0 auto;
		max-width: 60rem;
	}
</style>
