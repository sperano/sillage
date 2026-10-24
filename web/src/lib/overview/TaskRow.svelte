<!-- One task's row: its name (a link to its story), one lane with sparse
     labels, or once expanded a labelled lane per run, pull request and
     review wait; and its state at the window's end. -->
<script lang="ts">
	import { rootLabel } from '../roots';
	import { rootHref } from '../routes';
	import { endChips } from './end';
	import { collapsedLane, expandedLanes, NO_EXECUTOR, rowTitle } from './lanes';
	import LanePlot from './LanePlot.svelte';
	import type { OverviewRow } from './model';
	import type { Range } from './spans';
	import { rowUnknownSpans } from './unknown';

	interface Props {
		row: OverviewRow;
		range: Range;
		cursor: number;
		onpick: (t: number) => void;
	}

	let { row, range, cursor, onpick }: Props = $props();

	const id = $props.id();
	let expanded = $state(false);
	const title = $derived(rowTitle(row));
	const executorsText = $derived.by(() => {
		if (row.runs.length === 0) return 'no run in this window';
		const names = [...new Set(row.runs.flatMap((r) => (r.agent ? [r.agent] : [])))];
		if (row.runs.some((r) => !r.agent)) names.push(NO_EXECUTOR);
		return names.join(', ');
	});
	const unknown = $derived(rowUnknownSpans(row, range));
	const lanes = $derived(expanded ? expandedLanes(row, range) : [collapsedLane(row)]);
	const chips = $derived(endChips(row, range.end));
</script>

<li class="row" class:missing={row.missingHistory}>
	<div class="name">
		<a href={rootHref(row.root)}>{title}</a>
		<span class="sub">{rootLabel(row.root)}</span>
		<span class="sub">{executorsText}</span>
		<button
			type="button"
			class="toggle"
			aria-expanded={expanded}
			aria-controls="{id}-lanes"
			onclick={() => (expanded = !expanded)}
			><span aria-hidden="true">{expanded ? '▾' : '▸'}</span> Details<span class="visually-hidden">
				of {title}</span
			></button
		>
	</div>
	<div class="lanes" id="{id}-lanes">
		{#each lanes as lane (lane.key)}
			{#if expanded}<span class="lane-label">{lane.label}</span>{/if}
			<LanePlot {lane} {range} {cursor} {unknown} labelled={expanded} {onpick} />
		{/each}
		{#if row.missingHistory}
			<span class="lane-label">History incomplete in this window: nothing is measured there.</span>
		{/if}
	</div>
	<ul class="chips" aria-label="State of {title} at the window's end">
		{#each chips as chip, i (i)}
			<li class="chip {chip.kind}" title={chip.note}>
				{chip.text}{#if chip.note}<span class={expanded ? 'note' : 'visually-hidden'}
						>: {chip.note}</span
					>{/if}
			</li>
		{/each}
	</ul>
</li>

<style>
	.row {
		display: grid;
		grid-template-columns: var(--ov-name-width) 1fr var(--ov-state-width);
		gap: 0.5rem;
		padding: 0.4rem 0;
		border-top: 1px solid var(--ov-grid);
		align-items: start;
	}
	.name {
		display: flex;
		flex-direction: column;
		font-size: 0.85rem;
		min-width: 0;
	}
	.name a {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.sub,
	.lane-label {
		color: var(--ov-muted);
		font-size: 0.7rem;
	}
	.toggle {
		align-self: start;
		font-size: 0.7rem;
		margin-top: 0.2rem;
	}
	.chips {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 0.2rem;
	}
	.chip {
		font-size: 0.7rem;
		border-radius: 0.6rem;
		padding: 0 0.45rem;
		background: color-mix(in srgb, var(--ov-muted) 15%, var(--ov-bg));
	}
	.chip.running,
	.chip.waiting {
		background: color-mix(in srgb, var(--ov-running) 25%, var(--ov-bg));
	}
	.chip.review {
		background: color-mix(in srgb, var(--ov-review) 25%, var(--ov-bg));
	}
	.chip.failed,
	.chip.pr-closed,
	.chip.not-done {
		background: color-mix(in srgb, var(--ov-failure) 20%, var(--ov-bg));
	}
	.note {
		color: var(--ov-muted);
	}
	.chip.missing {
		border: 1px dashed var(--ov-unknown);
	}
	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
</style>
