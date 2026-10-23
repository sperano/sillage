<script lang="ts">
	import type { Api } from './lib/api';
	import type { Router } from './lib/router.svelte';
	import type { Route } from './lib/routes';
	import EntryPage from './pages/EntryPage.svelte';
	import NotFoundPage from './pages/NotFoundPage.svelte';
	import OpenPage from './pages/OpenPage.svelte';
	import StoryPage from './pages/StoryPage.svelte';

	let { api, router }: { api: Api; router: Router } = $props();

	const route = $derived(router.route);
	let main: HTMLElement | undefined = $state();

	$effect(() => router.start());

	// The page the route showed when the effect below last ran: plain, since
	// only that effect reads and writes it.
	let shown: Route['name'] | undefined;

	// After a navigation, move focus to the new page's heading, so that a
	// keyboard or screen reader user lands on what replaced the link. A new
	// search keeps the entry page, and the focus where the reader is.
	$effect(() => {
		const samePage = shown === 'home' && route.name === 'home';
		shown = route.name;
		if (router.navigations > 0 && !samePage) main?.querySelector<HTMLElement>('h1')?.focus();
	});
</script>

<svelte:document onclick={(event) => router.handleClick(event)} />

{#if route.name !== 'home'}
	<header><a href="/">Sillage</a></header>
{/if}
<main bind:this={main}>
	{#if route.name === 'home'}
		<EntryPage {api} {router} query={route.query} />
	{:else if route.name === 'task'}
		<StoryPage {api} root={{ task: route.key }} />
	{:else if route.name === 'page'}
		<StoryPage {api} root={{ page: route.key }} />
	{:else if route.name === 'open'}
		<OpenPage {api} {router} url={route.url} />
	{:else}
		<NotFoundPage path={route.path} />
	{/if}
</main>

<style>
	:global(body) {
		font-family: system-ui, sans-serif;
		line-height: 1.5;
		margin: 0 auto;
		max-width: 60rem;
		padding: 1rem;
	}
</style>
