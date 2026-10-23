<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import EventGlyph from './EventGlyph.svelte';
	import { spanDescription, spanText } from './intervals';
	import { activation } from './keys';
	import { LABEL_LINE_GAP_PX, labelWidth, type Bar } from './layout';
	import { formatClock, type Zone } from './time';

	interface Props {
		bar: Bar;
		/** The vertical centre of the bar's track. */
		y: number;
		/** Where the exit label's first line starts; unset when it is hidden. */
		label?: { x: number; y: number };
		zone: Zone;
		selection: Selection;
		/** Prefix of the ids of the chart's fill patterns. */
		patterns: string;
	}

	let { bar, y, label, zone, selection, patterns }: Props = $props();

	const BAR_PX = 16;
	/** Room kept between a bar's edge and its text. */
	const TEXT_PAD_PX = 5;
	/** The size of the chevron that marks an open end. */
	const CHEVRON_PX = 5;
	/** How far above the bar a stub's text sits. */
	const STUB_TEXT_RAISE_PX = 2;

	const span = $derived(bar.span);
	const text = $derived(spanText(span));
	const description = $derived(spanDescription(span, zone));
	const fits = $derived(labelWidth(text) + 2 * TEXT_PAD_PX <= bar.x2 - bar.x1);
	const open = $derived(span.endState === 'open');
	const top = $derived(y - BAR_PX / 2);
	const fill = $derived(
		span.phase === 'waiting'
			? `url(#${patterns}-waiting)`
			: span.phase === 'review-wait'
				? `url(#${patterns}-review)`
				: undefined
	);
	const selected = $derived(selection.is(span.selected));
	const select = () => selection.select(span.selected);
	// A stub starts at its known or open end and leaves towards the unknown one.
	const unknownSide = $derived(span.start === undefined ? 'start' : 'end');
	const stubText = $derived(
		[
			span.text,
			span.start === undefined ? 'start unknown' : '',
			open ? 'not ended at the last read' : span.end === undefined ? 'end unknown' : ''
		]
			.filter(Boolean)
			.join(', ')
	);
	/** Where the stub's drawn end is: its start, or its end when the start is unknown. */
	const stubEnd = $derived(unknownSide === 'start' ? bar.x2 : bar.x1);
</script>

<g
	class="span {span.phase}"
	class:selected
	role="button"
	tabindex="0"
	aria-label={description}
	aria-pressed={selected}
	data-span={span.phase}
	data-start={span.startState}
	data-end={span.endState}
	onclick={select}
	{...activation(select)}
>
	<title>{description}</title>
	{#if bar.shape === 'bar'}
		<rect
			class="fill"
			data-bar
			x={bar.x1}
			y={top}
			width={bar.x2 - bar.x1}
			height={BAR_PX}
			style:fill
		/>
		{#if open}
			<!-- An open end has no closing edge: three sides and a chevron. -->
			<path class="edge" d="M {bar.x2} {top} H {bar.x1} V {top + BAR_PX} H {bar.x2}" />
			<path
				class="edge"
				d="M {bar.x2} {y - CHEVRON_PX} L {bar.x2 + CHEVRON_PX} {y} L {bar.x2} {y + CHEVRON_PX}"
			/>
		{:else}
			<rect class="edge" x={bar.x1} y={top} width={bar.x2 - bar.x1} height={BAR_PX} />
		{/if}
		{#if fits}
			<text class="inside" x={bar.x1 + TEXT_PAD_PX} {y} dy="0.35em">{text}</text>
		{/if}
	{:else}
		<!-- One end unknown: no bar, a dashed stub from the other end. An open
		     end has no closing edge, only the chevron. -->
		<line class="stub" data-stub x1={bar.x1} y1={y} x2={bar.x2} y2={y} />
		{#if unknownSide === 'start' && open}
			<path
				class="edge"
				d="M {stubEnd} {y - CHEVRON_PX} L {stubEnd + CHEVRON_PX} {y} L {stubEnd} {y + CHEVRON_PX}"
			/>
		{:else}
			<line class="edge" x1={stubEnd} y1={top} x2={stubEnd} y2={top + BAR_PX} />
		{/if}
		<text
			class="outside"
			x={unknownSide === 'start' ? bar.x1 - TEXT_PAD_PX : bar.x2 + TEXT_PAD_PX}
			y={top - STUB_TEXT_RAISE_PX}
			text-anchor={unknownSide === 'start' ? 'end' : 'start'}>{stubText}</text
		>
	{/if}
	{#if span.exit}
		<EventGlyph mark={span.exit.mark} x={bar.x2} {y} />
		{#if label !== undefined}
			<text class="label" x={label.x} y={label.y}>{span.exit.label}</text>
			<text class="time" x={label.x} y={label.y + LABEL_LINE_GAP_PX}
				>{formatClock(span.exit.at, zone)}</text
			>
		{/if}
	{/if}
</g>

<style>
	.span {
		cursor: pointer;
		outline: none;
		--bar: var(--tl-running);
	}
	.waiting {
		--bar: var(--tl-waiting);
	}
	.review-wait {
		--bar: var(--tl-review);
	}
	.check {
		--bar: var(--tl-check);
	}
	.fill {
		fill: var(--bar);
		fill-opacity: 0.35;
	}
	.edge {
		fill: none;
		stroke: var(--bar);
		stroke-width: 1.5;
	}
	.review-wait .edge {
		stroke-dasharray: 1 3;
		stroke-width: 2;
	}
	.stub {
		stroke: var(--bar);
		stroke-width: 2;
		stroke-dasharray: 4 3;
	}
	.selected .edge,
	.selected .stub,
	.span:focus-visible .edge,
	.span:focus-visible .stub {
		stroke: var(--tl-selected);
		stroke-width: 3;
	}
	.span:focus-visible .edge,
	.span:focus-visible .stub {
		stroke: var(--tl-focus);
	}
	.inside,
	.outside {
		fill: var(--tl-fg);
		font-size: 11px;
	}
	.outside {
		fill: var(--tl-muted);
		font-style: italic;
	}
	.label {
		fill: var(--tl-fg);
		font-size: 11px;
		font-weight: 600;
	}
	.time {
		fill: var(--tl-muted);
		font-size: 10px;
	}
</style>
