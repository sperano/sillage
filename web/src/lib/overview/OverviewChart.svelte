<!-- The overview's chart: a time axis over the window, the cursor's slider,
     and a row per task, or a group per agent profile. -->
<script lang="ts">
	import { formatFull, formatMinute, MINUTE_MS, ticks } from '../timeline/time';
	import ExecutorGroupView from './ExecutorGroupView.svelte';
	import { byExecutor } from './lanes';
	import type { OverviewRow } from './model';
	import type { Grouping } from './query';
	import type { Range } from './spans';
	import TaskRow from './TaskRow.svelte';

	interface Props {
		rows: readonly OverviewRow[];
		range: Range;
		grouping: Grouping;
		/** The cursor's time, which the slider and a click on a lane move. */
		cursor: number;
	}

	let { rows, range, grouping, cursor = $bindable() }: Props = $props();

	/** The most axis ticks drawn: their labels must not touch. */
	const MAX_TICKS = 6;
	const PERCENT = 100;

	const id = $props.id();
	const axis = $derived(ticks(range.start, range.end, MAX_TICKS, 'utc'));
	const groups = $derived(grouping === 'executor' ? byExecutor(rows) : []);
	const pick = (t: number) => {
		cursor = Math.min(range.end, Math.max(range.start, t));
	};
</script>

<section class="chart" aria-label="Tasks over time">
	<div class="head">
		<span>{grouping === 'executor' ? 'Executor' : 'Task'}</span>
		<div class="axis" aria-hidden="true">
			{#each axis as tick (tick.at)}
				<span
					class="tick"
					style:left="{((tick.at - range.start) / (range.end - range.start)) * PERCENT}%"
					>{tick.label}</span
				>
			{/each}
		</div>
		<span>{grouping === 'executor' ? '' : `State at ${formatMinute(range.end, 'utc')} UTC`}</span>
	</div>
	<div class="head">
		<label for="{id}-cursor">Cursor, UTC</label>
		<input
			id="{id}-cursor"
			type="range"
			min={range.start}
			max={range.end}
			step={MINUTE_MS}
			value={cursor}
			aria-valuetext={formatFull(cursor, 'utc')}
			oninput={(e) => pick(Number(e.currentTarget.value))}
		/>
		<output for="{id}-cursor">{formatMinute(cursor, 'utc')}</output>
	</div>
	{#if grouping === 'executor'}
		<ol class="rows" aria-label="Runs by executor">
			{#each groups as group (group.executor ?? '')}
				<ExecutorGroupView {group} {range} {cursor} onpick={pick} />
			{/each}
		</ol>
	{:else}
		<ol class="rows" aria-label="Tasks">
			{#each rows as row (JSON.stringify(row.root))}
				<TaskRow {row} {range} {cursor} onpick={pick} />
			{/each}
		</ol>
	{/if}
	<p class="legend">
		Solid bar: running. Striped: waiting. Dotted: review requested. Outlined: pull request open.
		Chevron: not ended at the last read. Dashed stub: one end unknown. Hatched: no records known
		there, never counted as idle. ✓ task marked done, ◆ merged, ✕ closed without a merge, • another
		change. All times UTC.
	</p>
</section>

<style>
	.chart {
		--ov-name-width: 12rem;
		--ov-state-width: 10rem;
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
		color: var(--ov-fg);
		background: var(--ov-bg);
	}
	@media (prefers-color-scheme: dark) {
		.chart {
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
	.head {
		display: grid;
		grid-template-columns: var(--ov-name-width) 1fr var(--ov-state-width);
		gap: 0.5rem;
		align-items: center;
		font-size: 0.75rem;
		color: var(--ov-muted);
	}
	.axis {
		position: relative;
		height: 1.2rem;
	}
	.tick {
		position: absolute;
		transform: translateX(-50%);
		white-space: nowrap;
	}
	input[type='range'] {
		width: 100%;
		margin: 0;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.legend {
		color: var(--ov-muted);
		font-size: 0.75rem;
	}
</style>
