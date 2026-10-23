<script lang="ts">
	import { AXIS_PX } from './chart';
	import { ticks, type Zone } from './time';
	import type { Scale } from './view';

	interface Props {
		scale: Scale;
		zone: Zone;
		/** The bottom of the chart, where the grid lines end. */
		bottom: number;
	}

	let { scale, zone, bottom }: Props = $props();

	/** The least room, in pixels, between two ticks. */
	const TICK_SPACING_PX = 110;
	const LABEL_Y_PX = AXIS_PX - 10;

	const axisTicks = $derived(
		ticks(
			scale.window.start,
			scale.window.end,
			Math.max(1, Math.floor((scale.right - scale.left) / TICK_SPACING_PX)),
			zone
		)
	);
</script>

<g class="axis" aria-hidden="true">
	{#each axisTicks as tick (tick.at)}
		{@const x = scale.x(tick.at)}
		<line class="grid" x1={x} x2={x} y1={AXIS_PX - 4} y2={bottom} />
		<text {x} y={LABEL_Y_PX}>{tick.label}</text>
	{/each}
	<line class="base" x1={scale.left} x2={scale.right} y1={AXIS_PX} y2={AXIS_PX} />
</g>

<style>
	.grid {
		stroke: var(--tl-grid);
	}
	.base {
		stroke: var(--tl-muted);
	}
	text {
		fill: var(--tl-muted);
		font-size: 10px;
		text-anchor: middle;
	}
</style>
