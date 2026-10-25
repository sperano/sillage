<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import type { RowChart } from './chart';
	import EventGroup from './EventGroup.svelte';
	import EventPoint from './EventPoint.svelte';
	import {
		fitText,
		LABEL_BAND_PX,
		LINE_PX,
		SMALL_CHAR_PX,
		TITLE_X_PX,
		TRACK_PX,
		type LabelPlace
	} from './layout';
	import PhaseBar from './PhaseBar.svelte';
	import { PHASE_TRACK_PX } from './phases';
	import SpanBar from './SpanBar.svelte';
	import type { Zone } from './time';
	import type { Scale } from './view';

	interface Props {
		layout: RowChart;
		scale: Scale;
		zone: Zone;
		selection: Selection;
		/** Prefix of the ids of the chart's patterns and clip path. */
		ids: string;
		ontoggle: (id: string) => void;
	}

	let { layout, scale, zone, selection, ids, ontoggle }: Props = $props();

	/** Room kept between the left column's text and the plot. */
	const COLUMN_GUTTER_PX = 20;
	/** The space between the title, subtitle and note lines. */
	const SUBTITLE_GAP_PX = 14;

	const row = $derived(layout.row);
	/** Where a label's first line is drawn, or undefined for a hidden label. */
	const labelAt = (label: LabelPlace | undefined) =>
		label === undefined
			? undefined
			: { x: label.x, y: layout.labelY + label.level * LABEL_BAND_PX };
	const titleWidth = $derived(scale.left - TITLE_X_PX - COLUMN_GUTTER_PX);
</script>

<g class="row" data-row={row.id} data-section={row.section}>
	<line
		class="rule"
		x1="0"
		x2={scale.right}
		y1={layout.y + layout.height}
		y2={layout.y + layout.height}
	/>
	<text class="title" x={TITLE_X_PX} y={layout.markY} dy="0.35em">
		<title>{row.title}</title>{fitText(row.title, titleWidth)}
	</text>
	<text class="subtitle" x={TITLE_X_PX} y={layout.markY + SUBTITLE_GAP_PX} dy="0.35em">
		<title>{row.subtitle}</title>{fitText(row.subtitle, titleWidth, SMALL_CHAR_PX)}
	</text>
	{#if row.note}
		<text class="note" data-note x={TITLE_X_PX} y={layout.markY + 2 * SUBTITLE_GAP_PX} dy="0.35em">
			<title>{row.note}</title>{fitText(row.note, titleWidth, SMALL_CHAR_PX)}
		</text>
	{/if}
	<g clip-path="url(#{ids}-plot)">
		{#each layout.unknown as u, i (i)}
			<!-- The source bands above name these spans for assistive technology. -->
			<rect
				class="unknown"
				data-unknown
				aria-hidden="true"
				x={scale.x(u.start)}
				y={layout.y}
				width={Math.max(scale.x(u.end) - scale.x(u.start), 0)}
				height={layout.height}
				style:fill="url(#{ids}-hatch)"
			>
				<title>{u.reason}</title>
			</rect>
		{/each}
		{#each layout.bars as bar (bar.span.key)}
			<SpanBar
				{bar}
				y={layout.markY + bar.track * TRACK_PX}
				label={labelAt(bar.exitLabel)}
				{zone}
				{selection}
				patterns={ids}
			/>
		{/each}
		{#each layout.phases as placed (placed.span.key)}
			<PhaseBar {placed} y={layout.phaseY + placed.track * PHASE_TRACK_PX} {zone} {selection} />
		{/each}
		{#each layout.placed as p (p.kind === 'point' ? p.point.key : p.id)}
			{#if p.kind === 'point'}
				<EventPoint point={p.point} y={layout.markY} label={labelAt(p.label)} {zone} {selection} />
			{:else}
				<EventGroup
					id={p.id}
					x={p.x}
					members={p.members}
					expanded={p.expanded}
					y={layout.markY}
					listY={layout.listY + p.listLine * LINE_PX}
					{zone}
					{selection}
					{ontoggle}
				/>
			{/if}
		{/each}
	</g>
</g>

<style>
	.rule {
		stroke: var(--tl-grid);
	}
	.title {
		fill: var(--tl-fg);
		font-size: 12px;
		font-weight: 600;
	}
	.subtitle {
		fill: var(--tl-muted);
		font-size: 10px;
	}
	.note {
		fill: var(--tl-muted);
		font-size: 11px;
		font-style: italic;
	}
</style>
