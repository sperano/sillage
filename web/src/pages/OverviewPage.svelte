<!-- /overview: what was working on what, across tasks, over a window. -->
<script lang="ts">
	import { isAbort, type Api } from '../lib/api';
	import { count } from '../lib/format';
	import { load, type Load } from '../lib/load.svelte';
	import type { OverviewPage as Page } from '../lib/overview/model';
	import OverviewChart from '../lib/overview/OverviewChart.svelte';
	import OverviewFilters from '../lib/overview/OverviewFilters.svelte';
	import OverviewSummary from '../lib/overview/OverviewSummary.svelte';
	import type { OverviewQuery } from '../lib/overview/query';
	import type { Router } from '../lib/router.svelte';
	import { formatDate, formatMinute, MINUTE_MS } from '../lib/timeline/time';
	import ErrorNotice from '../components/ErrorNotice.svelte';

	let { api, router, query }: { api: Api; router: Router; query: OverviewQuery } = $props();

	const first = load(() => {
		const q = query;
		return (signal) => api.overview(q, undefined, signal);
	});

	// The pages after the first, for the first page's window; dropped, and
	// their request aborted, whenever the first page loads again.
	let later: Page[] = $state.raw([]);
	let more: Load<Page> = $state.raw({ state: 'idle' });
	let controller: AbortController | undefined;
	$effect(() => {
		void first.current;
		later = [];
		more = { state: 'idle' };
		return () => controller?.abort();
	});

	const firstPage = $derived(first.current.state === 'loaded' ? first.current.value : undefined);
	const rows = $derived(firstPage ? [firstPage, ...later].flatMap((p) => p.rows) : []);
	const next = $derived((later.at(-1) ?? firstPage)?.next);
	const range = $derived(
		firstPage
			? { start: Date.parse(firstPage.window.from), end: Date.parse(firstPage.window.to) }
			: { start: 0, end: 0 }
	);
	// The last minute of the window, until the reader moves it.
	let cursor = $derived(Math.max(range.start, range.end - MINUTE_MS));
	const executors = $derived(
		[
			...new Set(rows.flatMap((r) => r.runs.flatMap((run) => (run.agent ? [run.agent] : []))))
		].sort()
	);
	const filtered = $derived(
		query.project !== '' ||
			query.task !== '' ||
			query.executor !== '' ||
			query.review ||
			query.failed ||
			query.missing
	);

	async function loadMore() {
		if (!firstPage || !next) return;
		controller?.abort();
		controller = new AbortController();
		const q = { ...query, from: firstPage.window.from, to: firstPage.window.to };
		more = { state: 'loading' };
		try {
			later = [...later, await api.overview(q, next, controller.signal)];
			more = { state: 'idle' };
		} catch (error) {
			if (!isAbort(error)) more = { state: 'failed', error };
		}
	}

	const when = (t: number) => `${formatDate(t, 'utc')} ${formatMinute(t, 'utc')}`;
</script>

<svelte:head><title>What was working on what · Sillage</title></svelte:head>

<h1 tabindex="-1">What was working on what?</h1>
<p>
	Across tasks: runs, pull requests and review requests over a window. Open a task for its full
	history; move the cursor to see what was executing at a moment.
</p>
<OverviewFilters {router} {query} {executors} />

{#if firstPage}
	<p role="status">
		From {when(range.start)} to {when(range.end)} UTC: {count(firstPage.total, 'task')}{filtered
			? ' matching the filters'
			: ''}{next ? `, ${rows.length} loaded` : ''}.
	</p>
	{#if rows.length > 0}
		<OverviewSummary {rows} {range} {cursor} partial={next !== undefined} />
		<OverviewChart {rows} {range} grouping={query.group} bind:cursor />
	{/if}
	{#if next}
		<button type="button" onclick={loadMore} disabled={more.state === 'loading'}>
			{more.state === 'loading' ? 'Loading more tasks…' : 'Load more tasks'}
		</button>
	{/if}
	{#if more.state === 'failed'}
		<ErrorNotice error={more.error} notFound="The next tasks are not available." />
	{/if}
{:else if first.current.state === 'failed'}
	<ErrorNotice error={first.current.error} notFound="The overview is not available." />
{:else}
	<p role="status">Loading the overview…</p>
{/if}
