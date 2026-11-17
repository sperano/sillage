<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import { activation } from './keys';
	import { labelWidth } from './layout';
	import { CHEVRON_ROOM_PX, phaseDescription, TICK_TEXT_PAD_PX, type PlacedPhase } from './phases';
	import type { Zone } from './time';

	interface Props {
		placed: PlacedPhase;
		/** The vertical centre of the phase's sub-track. */
		y: number;
		zone: Zone;
		selection: Selection;
	}

	let { placed, y, zone, selection }: Props = $props();

	/** The height of a tick and of a segment. */
	const TICK_PX = 12;
	/** The size of the chevron after an in-progress phase's tick. */
	const CHEVRON_PX = 4;
	/** Room kept between a segment's ends and its text. */
	const TEXT_PAD_PX = 3;

	const span = $derived(placed.span);
	const description = $derived(phaseDescription(span, zone));
	const selected = $derived(selection.isSelected(span.selected));
	const select = () => selection.select(span.selected);
	const fits = $derived(labelWidth(span.text) + 2 * TEXT_PAD_PX <= placed.x2 - placed.x1);
	/** The x of each recorded edge drawn as a tick: both of a pair, else its one. */
	const ticks = $derived(span.shape === 'pair' ? [placed.x1, placed.x2] : [placed.x1]);
</script>

<g
	class="phase {span.shape}"
	class:selected
	class:uncertain={span.uncertain}
	role="button"
	tabindex="0"
	aria-label={description}
	aria-pressed={selected}
	data-phase={span.phase.state}
	data-confidence={span.phase.confidence}
	onclick={select}
	{...activation(select)}
>
	<title>{description}</title>
	{#if span.shape === 'segment'}
		<!-- Both ends recorded: a dashed segment between them. -->
		<rect
			class="segment"
			data-segment
			x={placed.x1}
			y={y - TICK_PX / 2}
			width={placed.x2 - placed.x1}
			height={TICK_PX}
		/>
		{#if fits}
			<text class="inside" x={placed.x1 + TEXT_PAD_PX} {y} dy="0.35em">{span.text}</text>
		{/if}
	{:else}
		<!-- One edge recorded, or two that do not pair: ticks only, no segment. -->
		{#each ticks as x, i (i)}
			<line class="edge" data-tick x1={x} y1={y - TICK_PX / 2} x2={x} y2={y + TICK_PX / 2} />
		{/each}
		{#if span.phase.state === 'in-progress'}
			<path
				class="edge"
				data-chevron
				d="M {placed.x2 + CHEVRON_PX} {y - CHEVRON_PX} L {placed.x2 +
					2 * CHEVRON_PX} {y} L {placed.x2 + CHEVRON_PX} {y + CHEVRON_PX}"
			/>
		{/if}
		{#if span.shape === 'end'}
			<text class="tick-text" x={placed.x1 - TICK_TEXT_PAD_PX} {y} dy="0.35em" text-anchor="end"
				>{span.text}</text
			>
		{:else}
			<text
				class="tick-text"
				x={placed.x2 +
					TICK_TEXT_PAD_PX +
					(span.phase.state === 'in-progress' ? CHEVRON_ROOM_PX : 0)}
				{y}
				dy="0.35em">{span.text}</text
			>
		{/if}
	{/if}
</g>

<style>
	.phase {
		cursor: pointer;
		outline: none;
		--phase: var(--color-running);
	}
	.uncertain {
		--phase: var(--color-unknown);
	}
	.segment {
		fill: var(--phase);
		fill-opacity: 0.15;
		stroke: var(--phase);
		stroke-width: 1.5;
		stroke-dasharray: 4 2;
	}
	.uncertain .segment {
		stroke-dasharray: 2 3;
	}
	.edge {
		fill: none;
		stroke: var(--phase);
		stroke-width: 1.5;
	}
	.selected .segment,
	.selected .edge {
		stroke: var(--color-selected);
	}
	.phase:focus-visible .segment,
	.phase:focus-visible .edge {
		stroke: var(--color-focus);
		stroke-width: 3;
	}
	.inside,
	.tick-text {
		fill: var(--color-muted);
		font-size: 10px;
	}
	.uncertain .inside,
	.uncertain .tick-text {
		font-style: italic;
	}
</style>
