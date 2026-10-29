<script lang="ts">
	import { GLYPH_R_PX, SELECTED_RING_R_PX, type MarkKind } from './marks';

	interface Props {
		mark: MarkKind;
		x: number;
		y: number;
		selected?: boolean;
	}

	let { mark, x, y, selected = false }: Props = $props();

	/** Half the glyph's size, short for the shapes below. */
	const R = GLYPH_R_PX;
	/** A check mark drawn inside a glyph. */
	const TICK_PATH = `M ${-R * 0.6} 0 L ${-R * 0.2} ${R / 2} L ${R * 0.6} ${-R / 2}`;
	/** How far a run exit's flagpole reaches past the glyph, and its flag's depth. */
	const FLAG_PX = 3;
	/** How much larger than the others the merge diamond is drawn. */
	const DIAMOND_GROW_PX = 1;
	/** A cross drawn inside a glyph. */
	const CROSS_PATH = `M ${-R / 2} ${-R / 2} L ${R / 2} ${R / 2} M ${R / 2} ${-R / 2} L ${-R / 2} ${R / 2}`;
</script>

<!-- Each kind of mark has its own shape: an ending is never just a dot. -->
<g class="glyph {mark}" transform="translate({x} {y})" data-mark={mark}>
	{#if selected}
		<circle class="ring" r={SELECTED_RING_R_PX} />
	{/if}
	{#if mark === 'observed'}
		<circle class="hollow" r={R} />
	{:else if mark === 'task-done'}
		<rect class="solid" x={-R} y={-R} width={R * 2} height={R * 2} />
		<path class="tick" d={TICK_PATH} />
	{:else if mark === 'run-exit'}
		<path
			class="solid"
			d="M 0 {-R - FLAG_PX} L 0 {R + FLAG_PX} M 0 {-R - FLAG_PX} L {R + FLAG_PX - 1} {-R} L 0 {-R +
				FLAG_PX}"
		/>
	{:else if mark === 'check-success'}
		<circle class="solid" r={R} />
		<path class="tick" d={TICK_PATH} />
	{:else if mark === 'check-end'}
		<circle class="solid" r={R} />
		<path class="tick" d={CROSS_PATH} />
	{:else if mark === 'pr-merged'}
		<path
			class="solid"
			d="M 0 {-R - DIAMOND_GROW_PX} L {R + DIAMOND_GROW_PX} 0 L 0 {R + DIAMOND_GROW_PX} L {-R -
				DIAMOND_GROW_PX} 0 Z"
		/>
	{:else if mark === 'pr-closed'}
		<rect class="hollow" x={-R} y={-R} width={R * 2} height={R * 2} />
		<path class="cross" d="M {-R} {-R} L {R} {R} M {R} {-R} L {-R} {R}" />
	{:else}
		<circle class="solid" r={R} />
	{/if}
</g>

<style>
	.glyph {
		--glyph: var(--color-event);
	}
	.observed {
		--glyph: var(--color-event);
	}
	.task-done,
	.check-success {
		--glyph: var(--color-success);
	}
	.check-end,
	.pr-closed {
		--glyph: var(--color-failure);
	}
	.pr-merged {
		--glyph: var(--color-merged);
	}
	.run-exit {
		--glyph: var(--color-fg);
	}
	.solid {
		fill: var(--glyph);
		stroke: var(--glyph);
		stroke-width: 2;
	}
	.hollow {
		fill: var(--color-surface);
		stroke: var(--glyph);
		stroke-width: 2;
	}
	.tick,
	.cross {
		fill: none;
		stroke: var(--color-surface);
		stroke-width: 1.6;
	}
	.cross {
		stroke: var(--glyph);
	}
	.run-exit .solid {
		fill: var(--glyph);
	}
	.ring {
		fill: none;
		stroke: var(--color-selected);
		stroke-width: 2.5;
	}
</style>
