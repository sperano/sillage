<!-- /overview: what was working on what, across tasks, over a window. -->
<script lang="ts">
	import { isAbort, type Api } from '../lib/api';
	import { count } from '../lib/format';
	import { load } from '../lib/load.svelte';
	import type { OverviewPage as Page } from '../lib/overview/model';
	import OverviewChart from '../lib/overview/OverviewChart.svelte';
	import OverviewFilters from '../lib/overview/OverviewFilters.svelte';
	import OverviewSummary from '../lib/overview/OverviewSummary.svelte';
	import type { OverviewQuery } from '../lib/overview/query';
	import { sameRoot } from '../lib/roots';
	import type { Router } from '../lib/router.svelte';
	import { formatDate, formatMinute, MINUTE_MS } from '../lib/timeline/time';
	import ErrorNotice from '../components/ErrorNotice.svelte';

	let { api, router, query }: { api: Api; router: Router; query: OverviewQuery } = $props();

	const first = load(() => {
		const q = query;
		return (signal) => api.overview(q, undefined, signal);
	});

	const firstPage = $derived(first.current.state === 'loaded' ? first.current.value : undefined);

	// The pages loaded after a first page: they extend that page only, so a
	// new first page starts without them.
	let extra: { after: Page; pages: Page[] } | undefined = $state.raw();
	const later = $derived(extra !== undefined && extra.after === firstPage ? extra.pages : []);
	/** The next page's request: in flight, or failed. */
	let more: { state: 'idle' | 'loading' } | { state: 'failed'; error: unknown } = $state.raw({
		state: 'idle'
	});
	let controller: AbortController | undefined;
	// Leaving the page aborts the next page's request.
	$effect(() => () => controller?.abort());

	// A task that moved between two pages' requests is shown once, where it was first.
	const rows = $derived(
		firstPage
			? [firstPage, ...later]
					.flatMap((p) => p.rows)
					.filter((r, i, all) => all.findIndex((o) => sameRoot(o.root, r.root)) === i)
			: []
	);
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
		const base = firstPage;
		if (!base || !next) return;
		controller?.abort();
		controller = new AbortController();
		const signal = controller.signal;
		const q = { ...query, from: base.window.from, to: base.window.to };
		more = { state: 'loading' };
		try {
			const page = await api.overview(q, next, signal);
			// A first page that changed meanwhile drops what extended the old one.
			if (base === firstPage) extra = { after: base, pages: [...later, page] };
			more = { state: 'idle' };
		} catch (error) {
			if (!isAbort(error) && !signal.aborted) more = { state: 'failed', error };
		}
	}

	const when = (t: number) => `${formatDate(t, 'utc')} ${formatMinute(t, 'utc')}`;
</script>

<svelte:head><title>What was working on what · Sillage</title></svelte:head>

<div class="overview">
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
</div>

<style>
	.overview {
		--ov-bg: #ffffff;
		--ov-fg: #1d2733;
		--ov-muted: #5b6b7c;
		--ov-grid: #d9e0e8;
		--ov-running: #2a7f86;
		--ov-on-running: #ffffff;
		--ov-waiting: #a86b00;
		--ov-review: #6f42c1;
		--ov-pr: #1f5f9e;
		--ov-failure: #b3261e;
		--ov-unknown: #8a96a3;
		--ov-mark: #1d2733;
		--ov-cursor: #d97706;
		color-scheme: light dark;
		color: var(--ov-fg);
		background: var(--ov-bg);
	}
	@media (prefers-color-scheme: dark) {
		.overview {
			--ov-bg: #10161d;
			--ov-fg: #e3e9ef;
			--ov-muted: #9aa8b6;
			--ov-grid: #2b3743;
			--ov-running: #5cc2ca;
			--ov-on-running: #10161d;
			--ov-waiting: #e0a84a;
			--ov-review: #b99af0;
			--ov-pr: #7ab3ef;
			--ov-failure: #f2867e;
			--ov-unknown: #6b7884;
			--ov-mark: #e3e9ef;
			--ov-cursor: #f59e0b;
		}
	}
</style>
