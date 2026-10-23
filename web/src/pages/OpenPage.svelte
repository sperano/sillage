<!-- /open?url=...: resolves a link to its root, then shows that root's story in its place. -->
<script lang="ts">
	import type { Api } from '../lib/api';
	import { load } from '../lib/load.svelte';
	import type { Router } from '../lib/router.svelte';
	import { rootHref } from '../lib/routes';
	import ErrorNotice from '../components/ErrorNotice.svelte';

	let { api, router, url }: { api: Api; router: Router; url: string } = $props();

	const link = $derived(url.trim());
	const resolved = load(() => {
		const l = link;
		return l === '' ? undefined : (signal) => api.resolve(l, signal);
	});

	$effect(() => {
		const r = resolved.current;
		// Replace, so that going back leaves the story rather than resolving again.
		if (r.state === 'loaded') router.navigate(rootHref(r.value), { replace: true });
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
		notFound="Sillage knows no task or page for this link: it may not have been read yet, or sillage does not follow links of this kind."
	/>
	<p><a href="/">Paste another link or search</a></p>
{:else}
	<p role="status">Looking up <code>{url}</code>…</p>
{/if}
