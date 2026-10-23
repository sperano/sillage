<script lang="ts">
	import type { Api } from './lib/api';
	import type { Router } from './lib/router.svelte';
	import EntryPage from './pages/EntryPage.svelte';
	import NotFoundPage from './pages/NotFoundPage.svelte';
	import OpenPage from './pages/OpenPage.svelte';
	import StoryPage from './pages/StoryPage.svelte';

	let { api, router }: { api: Api; router: Router } = $props();

	const route = $derived(router.route);

	$effect(() => router.start());
</script>

<svelte:document onclick={(event) => router.handleClick(event)} />

{#if route.name !== 'home'}
	<header><a href="/">Sillage</a></header>
{/if}
<main>
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
