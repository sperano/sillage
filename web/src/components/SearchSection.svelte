<!-- The entry page's search: tasks and pages by title or key. -->
<script lang="ts">
	import { MAX_SEARCH_LENGTH, type Api } from '../lib/api';
	import { formatTime } from '../lib/format';
	import { load } from '../lib/load.svelte';
	import { rootLabel } from '../lib/roots';
	import type { Router } from '../lib/router.svelte';
	import { QUERY_PARAM, homeHref, rootHref } from '../lib/routes';
	import ErrorNotice from './ErrorNotice.svelte';

	let { api, router, query }: { api: Api; router: Router; query: string } = $props();

	const uid = $props.id();
	const wanted = $derived(query.trim());
	const results = load(() => {
		const q = wanted;
		return q === '' ? undefined : (signal) => api.search(q, signal);
	});

	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		const q = new FormData(event.currentTarget).get(QUERY_PARAM);
		router.navigate(homeHref(typeof q === 'string' ? q.trim() : ''));
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

	{#if results.current.state === 'loading'}
		<p role="status">Searching…</p>
	{:else if results.current.state === 'failed'}
		<ErrorNotice error={results.current.error} notFound="The search is not available." />
	{:else if results.current.state === 'loaded'}
		{@const hits = results.current.value}
		{#if hits.length === 0}
			<p role="status">No task or page matches “{wanted}”.</p>
		{:else}
			<ul aria-label="Search results">
				{#each hits as hit (rootHref(hit.root))}
					<li>
						<a href={rootHref(hit.root)}>{hit.title ?? rootLabel(hit.root)}</a>
						{#if hit.title}<span class="key">{rootLabel(hit.root)}</span>{/if}
						{#if hit.lastActivity}
							<span class="activity">
								last activity
								<time datetime={hit.lastActivity}>{formatTime(hit.lastActivity)}</time>
							</span>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</section>

<style>
	.key,
	.activity {
		color: #555;
		margin-left: 0.5rem;
	}
</style>
