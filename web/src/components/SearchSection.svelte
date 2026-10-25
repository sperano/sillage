<!-- The entry page's search: tasks and pages by title or key. -->
<script lang="ts">
	import { MAX_SEARCH_LENGTH, type Api } from '../lib/api';
	import { load } from '../lib/load.svelte';
	import type { Router } from '../lib/router.svelte';
	import { QUERY_PARAM, homeHref } from '../lib/routes';
	import ErrorNotice from './ErrorNotice.svelte';
	import RootList from './RootList.svelte';

	let { api, router, query }: { api: Api; router: Router; query: string } = $props();

	const uid = $props.id();
	const wanted = $derived(query.trim());
	// A link can carry a search longer than the box accepts: it is not sent.
	const tooLong = $derived(wanted.length > MAX_SEARCH_LENGTH);
	const results = load(() => {
		const q = wanted;
		return q === '' || tooLong ? undefined : (signal) => api.search(q, signal);
	});

	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		const field = new FormData(event.currentTarget).get(QUERY_PARAM);
		const q = typeof field === 'string' ? field.trim() : '';
		// The same search again is the reader retrying: the URL stays, the request runs again.
		if (q === wanted) results.reload();
		else router.navigate(homeHref(q));
	}
</script>

<section aria-labelledby="{uid}-heading">
	<h2 id="{uid}-heading">Find a task or a page</h2>
	<form role="search" onsubmit={submit}>
		<label>
			Title or key
			<input type="search" name={QUERY_PARAM} value={query} maxlength={MAX_SEARCH_LENGTH} />
		</label>
		<button type="submit">Search</button>
	</form>

	{#if tooLong}
		<p role="alert">
			A search is at most {MAX_SEARCH_LENGTH} characters long; this one has {wanted.length}.
		</p>
	{:else if results.current.state === 'loading'}
		<p role="status">Searching…</p>
	{:else if results.current.state === 'failed'}
		<ErrorNotice error={results.current.error} notFound="The search is not available." />
	{:else if results.current.state === 'loaded'}
		{@const hits = results.current.value}
		{#if hits.length === 0}
			<p role="status">No task or page matches “{wanted}”.</p>
		{:else}
			<RootList roots={hits} label="Search results" />
		{/if}
	{/if}
</section>
