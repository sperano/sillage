<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import EventGlyph from './EventGlyph.svelte';
	import { onActivate } from './keys';
	import type { Point } from './layout';
	import { actorLabel } from './marks';
	import { formatClock, formatFull, type Zone } from './time';

	interface Props {
		point: Point;
		y: number;
		/** Where the label's first line sits; unset when the label is hidden. */
		labelY?: number;
		zone: Zone;
		selection: Selection;
	}

	let { point, y, labelY, zone, selection }: Props = $props();

	/** The space between a label's two lines. */
	const LINE_GAP_PX = 12;
	/** The keyboard focus ring's radius. */
	const FOCUS_R = 11;

	const name = $derived(
		`${point.label}, ${actorLabel(point.event)}, ${formatFull(point.at, zone)}` +
			(point.event.sourceTime === undefined ? ', observed time' : '')
	);
	const selected = $derived(selection.is(point.selected));
	const select = () => selection.select(point.selected);
</script>

<g
	class="point"
	role="button"
	tabindex="0"
	aria-label={name}
	aria-pressed={selected}
	data-event={point.key}
	onclick={select}
	onkeydown={onActivate(select)}
>
	<title>{name}</title>
	<circle class="focus" cx={point.x} cy={y} r={FOCUS_R} />
	<EventGlyph mark={point.mark} x={point.x} {y} {selected} />
	{#if labelY !== undefined}
		<text class="label" x={point.x - 4} y={labelY}>{point.label}</text>
		<text class="time" x={point.x - 4} y={labelY + LINE_GAP_PX}>{formatClock(point.at, zone)}</text>
	{/if}
</g>

<style>
	.point {
		cursor: pointer;
		outline: none;
	}
	.focus {
		fill: none;
		stroke: var(--tl-focus);
		stroke-width: 2;
		stroke-dasharray: 3 2;
		opacity: 0;
	}
	.point:focus-visible .focus {
		opacity: 1;
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
