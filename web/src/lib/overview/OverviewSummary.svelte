<!-- What the loaded rows say at the cursor, at the window's end, and of
     their coverage. Each card names the time it is about: they summarize
     different moments. -->
<script lang="ts">
	import { count } from '../format';
	import { formatMinute } from '../timeline/time';
	import { rowTitles } from './lanes';
	import type { OverviewRow } from './model';
	import type { Range } from './spans';
	import { endChips } from './end';
	import { instantOf } from './state';

	interface Props {
		rows: readonly OverviewRow[];
		range: Range;
		cursor: number;
		/** Whether more rows match than are loaded. */
		partial: boolean;
	}

	let { rows, range, cursor, partial }: Props = $props();

	// Untitled tasks sharing a short name are told apart by their instance.
	const titles = $derived(rowTitles(rows));
	const names = (list: readonly OverviewRow[]) => list.map((r) => titles.get(r)).join(', ');

	const instant = $derived(instantOf(rows, cursor));
	const runs = $derived(instant.executing.reduce((n, e) => n + e.runs.length, 0));
	// As the rows' state chips say: a request not answered at the last read counts.
	const reviewing = $derived(
		rows.filter((r) => endChips(r, range.end).some((c) => c.kind === 'review'))
	);
	const missing = $derived(rows.filter((r) => r.missingHistory));
</script>

<div class="cards">
	<section class="card" aria-label="At the cursor">
		<h2>At the cursor · {formatMinute(cursor, 'utc')} UTC</h2>
		<p class="big">{count(runs, 'run')} executing</p>
		<p>
			{#if instant.executing.length > 0}
				{names(instant.executing.map((e) => e.row))}
			{:else}
				No run was recorded executing then.
			{/if}
		</p>
		{#if instant.unknown.length > 0}
			<p class="unknown">Unknown then: {names(instant.unknown)}</p>
		{/if}
	</section>
	<section class="card" aria-label="At the window's end">
		<h2>At the window's end · {formatMinute(range.end, 'utc')} UTC</h2>
		<p class="big">{count(reviewing.length, 'task')} with a review requested</p>
		<p>{reviewing.length > 0 ? names(reviewing) : 'No review request was waiting.'}</p>
	</section>
	<section class="card" aria-label="Coverage">
		<h2>Coverage</h2>
		<p class="big">{count(missing.length, 'task')} with incomplete history</p>
		<p>
			{missing.length > 0 ? `${names(missing)}. ` : ''}Unknown time is never counted as idle.
		</p>
	</section>
</div>
{#if partial}
	<p class="partial">These summaries cover the tasks loaded so far.</p>
{/if}

<style>
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
		gap: 0.5rem;
	}
	.card {
		border: 1px solid var(--ov-grid);
		border-radius: 0.4rem;
		padding: 0.4rem 0.6rem;
	}
	h2 {
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		margin: 0;
		color: var(--ov-muted);
	}
	p {
		margin: 0.1rem 0;
		font-size: 0.8rem;
	}
	.big {
		font-size: 1.05rem;
		font-weight: 600;
	}
	.unknown,
	.partial {
		color: var(--ov-muted);
		font-size: 0.75rem;
	}
</style>
