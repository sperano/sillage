<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import EventGlyph from './EventGlyph.svelte';
	import { activation } from './keys';
	import { LABEL_LINE_GAP_PX, type Point } from './layout';
	import { FOCUS_RING_R_PX } from './marks';
	import { formatClock, type Zone } from './time';

	interface Props {
		point: Point;
		y: number;
		/** Where the label's first line starts; unset when the label is hidden. */
		label?: { x: number; y: number };
		zone: Zone;
		selection: Selection;
	}

	let { point, y, label, zone, selection }: Props = $props();

	const name = $derived(point.name(zone));
	const kind = $derived(point.selected.kind);
	const selected = $derived(selection.isSelected(point.selected));
	const select = () => selection.select(point.selected);
</script>

<g
	class="point"
	role="button"
	tabindex="0"
	aria-label={name}
	aria-pressed={selected}
	data-event={kind === 'event' ? point.key : undefined}
	data-delivery={kind === 'delivery' ? point.key : undefined}
	onclick={select}
	{...activation(select)}
>
	<title>{name}</title>
	<circle class="focus" cx={point.x} cy={y} r={FOCUS_RING_R_PX} />
	<EventGlyph mark={point.mark} x={point.x} {y} {selected} />
	{#if label !== undefined}
		<text class="label" x={label.x} y={label.y}>{point.label}</text>
		<text class="time" x={label.x} y={label.y + LABEL_LINE_GAP_PX}
			>{formatClock(point.at, zone)}</text
		>
	{/if}
</g>

<style>
	.point {
		cursor: pointer;
		outline: none;
	}
	.focus {
		fill: none;
		stroke: var(--color-focus);
		stroke-width: 2;
		stroke-dasharray: 3 2;
		opacity: 0;
	}
	.point:focus-visible .focus {
		opacity: 1;
	}
	.label {
		fill: var(--color-fg);
		font-size: 11px;
		font-weight: 600;
	}
	.time {
		fill: var(--color-muted);
		font-size: 10px;
	}
</style>
