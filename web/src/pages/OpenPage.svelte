<!--
	/open?url=...: resolves a link to its root, then shows that root's story
	in its place; a link several roots hold lists them to choose from.
-->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { load } from '../lib/load.svelte';
	import type { Router } from '../lib/router.svelte';
	import { rootHref } from '../lib/routes';
	import ErrorNotice from '../components/ErrorNotice.svelte';
	import RootList from '../components/RootList.svelte';

	let { api, router, url }: { api: Api; router: Router; url: string } = $props();

	const link = $derived(url.trim());
	const resolved = load(() => {
		const l = link;
		return l === '' ? undefined : (signal) => api.resolve(l, signal);
	});

	$effect(() => {
		const r = resolved.current;
		if (r.state !== 'loaded') return;
		// One root is the link's story; several are listed to choose from.
		const [only, ...others] = r.value.roots;
		// Replace, so that going back leaves the story rather than resolving again.
		if (only && others.length === 0) router.navigate(rootHref(only.root), { replace: true });
	});
</script>

<svelte:head><title>Open a link · Sillage</title></svelte:head>

<h1 tabindex="-1">Open a link</h1>
{#if link === ''}
	<p>There is no link to open.</p>
	<p><a href="/">Paste a link or search</a></p>
{:else if resolved.current.state === 'failed'}
	<p>Link: <code>{url}</code></p>
	<ErrorNotice
		error={resolved.current.error}
		notFound="Sillage knows no task for this link: it may not have been read yet, or sillage does not follow links of this kind."
	/>
	<p><a href="/">Paste another link or search</a></p>
{:else if resolved.current.state === 'loaded' && resolved.current.value.roots.length > 1}
	<p>Link: <code>{url}</code></p>
	<p>Several tasks hold what this link points to, the most recently active first:</p>
	<RootList roots={resolved.current.value.roots} label="Tasks of the link" />
{:else}
	<p role="status">Looking up <code>{url}</code>…</p>
{/if}
