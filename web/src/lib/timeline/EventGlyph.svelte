<script lang="ts">
	import type { MarkKind } from './marks';

	interface Props {
		mark: MarkKind;
		x: number;
		y: number;
		selected?: boolean;
	}

	let { mark, x, y, selected = false }: Props = $props();

	/** Half the glyph's size. */
	const R = 5;
	/** The selection ring's radius. */
	const RING_R = 9;
</script>

<!-- Each kind of mark has its own shape: an ending is never just a dot. -->
<g class="glyph {mark}" transform="translate({x} {y})" data-mark={mark}>
	{#if selected}
		<circle class="ring" r={RING_R} />
	{/if}
	{#if mark === 'observed'}
		<circle class="hollow" r={R} />
	{:else if mark === 'task-done'}
		<rect class="solid" x={-R} y={-R} width={R * 2} height={R * 2} />
		<path class="tick" d="M -3 0 L -1 2.5 L 3 -2.5" />
	{:else if mark === 'run-exit'}
		<path class="solid" d="M 0 {-R - 3} L 0 {R + 3} M 0 {-R - 3} L {R + 2} {-R} L 0 {-R + 3}" />
	{:else if mark === 'check-success'}
		<circle class="solid" r={R} />
		<path class="tick" d="M -3 0 L -1 2.5 L 3 -2.5" />
	{:else if mark === 'check-end'}
		<circle class="solid" r={R} />
		<path class="tick" d="M -2.5 -2.5 L 2.5 2.5 M 2.5 -2.5 L -2.5 2.5" />
	{:else if mark === 'pr-merged'}
		<path class="solid" d="M 0 {-R - 1} L {R + 1} 0 L 0 {R + 1} L {-R - 1} 0 Z" />
	{:else if mark === 'pr-closed'}
		<rect class="hollow" x={-R} y={-R} width={R * 2} height={R * 2} />
		<path class="cross" d="M {-R} {-R} L {R} {R} M {R} {-R} L {-R} {R}" />
	{:else}
		<circle class="solid" r={R} />
	{/if}
</g>

<style>
	.glyph {
		--glyph: var(--tl-event);
	}
	.observed {
		--glyph: var(--tl-observed);
	}
	.task-done,
	.check-success {
		--glyph: var(--tl-success);
	}
	.check-end,
	.pr-closed {
		--glyph: var(--tl-failure);
	}
	.pr-merged {
		--glyph: var(--tl-merged);
	}
	.run-exit {
		--glyph: var(--tl-fg);
	}
	.solid {
		fill: var(--glyph);
		stroke: var(--glyph);
		stroke-width: 2;
	}
	.hollow {
		fill: var(--tl-bg);
		stroke: var(--glyph);
		stroke-width: 2;
	}
	.tick,
	.cross {
		fill: none;
		stroke: var(--tl-bg);
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
		stroke: var(--tl-selected);
		stroke-width: 2.5;
	}
</style>
