<!-- /overview: what was working on what, across tasks, over a window. -->
<script lang="ts">
	import { isAbort, type Api } from '../lib/api';
	import { count } from '../lib/format';
	import { load } from '../lib/load.svelte';
	import type { OverviewPage as Page } from '../lib/overview/model';
	import { lastCursor } from '../lib/overview/cursor';
	import OverviewChart from '../lib/overview/OverviewChart.svelte';
	import OverviewFilters from '../lib/overview/OverviewFilters.svelte';
	import OverviewSummary from '../lib/overview/OverviewSummary.svelte';
	import type { OverviewQuery } from '../lib/overview/query';
	import { sameRoot } from '../lib/roots';
	import type { Router } from '../lib/router.svelte';
	import { formatDate, formatMinute, zoneName, type Zone } from '../lib/timeline/time';
	import TimeZoneSelect from '../lib/timeline/TimeZoneSelect.svelte';
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
	type More = { state: 'idle' | 'loading' } | { state: 'failed'; error: unknown };
	// The next page's request, for the first page it extends: a new first
	// page starts idle, whatever happened to the old one's.
	let request: { after: Page; more: More } | undefined = $state.raw();
	const more: More = $derived(
		request !== undefined && request.after === firstPage ? request.more : { state: 'idle' }
	);
	let controller: AbortController | undefined;
	// A new first page, or leaving the page, aborts the next page's request.
	$effect(() => {
		void firstPage;
		return () => controller?.abort();
	});

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
	let cursor = $derived(lastCursor(range));
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
	// Keep the existing UTC view until the reader chooses local time.
	let zone = $state<Zone>('utc');

	async function loadMore() {
		const base = firstPage;
		if (!base || !next) return;
		controller?.abort();
		controller = new AbortController();
		const signal = controller.signal;
		const q = { ...query, from: base.window.from, to: base.window.to };
		request = { after: base, more: { state: 'loading' } };
		// Only the latest request for the first page shown settles: a newer
		// request, or a new first page, aborts this one; the first page is
		// compared too, since it changes before the effect that aborts runs.
		const current = () => !signal.aborted && base === firstPage;
		try {
			const page = await api.overview(q, next, signal);
			if (!current()) return;
			extra = { after: base, pages: [...later, page] };
			request = { after: base, more: { state: 'idle' } };
		} catch (error) {
			if (current() && !isAbort(error)) request = { after: base, more: { state: 'failed', error } };
		}
	}

	const when = (t: number) => `${formatDate(t, zone)} ${formatMinute(t, zone)}`;
	const shownRange = $derived(`From ${when(range.start)} to ${when(range.end)} ${zoneName(zone)}`);
</script>

<svelte:head><title>What was working on what · Sillage</title></svelte:head>

<div class="overview">
	<h1 tabindex="-1">What was working on what?</h1>
	<p>
		Across tasks: runs, pull requests and review requests over a window. Open a task for its full
		history; move the cursor to see what was executing at a moment.
	</p>
	<OverviewFilters {router} {query} {executors} />
	<TimeZoneSelect bind:zone />

	{#if firstPage}
		<p role="status">
			{shownRange}: {count(firstPage.total, 'task')}{filtered ? ' matching the filters' : ''}{next
				? `, ${rows.length} loaded`
				: ''}.
		</p>
		{#if rows.length > 0}
			<OverviewSummary {rows} {range} {cursor} {zone} partial={next !== undefined} />
			<OverviewChart {rows} {range} grouping={query.group} {zone} bind:cursor />
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
