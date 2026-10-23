<script lang="ts">
	import { SOURCE_ROW_PX, type SourceBand } from './chart';
	import { fitText, SMALL_CHAR_PX } from './layout';
	import type { Scale } from './view';

	interface Props {
		band: SourceBand;
		scale: Scale;
		/** Prefix of the ids of the chart's patterns and clip path. */
		ids: string;
	}

	let { band, scale, ids }: Props = $props();

	const TITLE_X_PX = 12;
	const TEXT_PAD_PX = 4;
	const INSET_PX = 3;
</script>

<!-- One source: shaded wherever its records are unknown, with the reason. -->
<g class="band" data-source={band.source}>
	<text class="title" x={TITLE_X_PX} y={band.y + SOURCE_ROW_PX / 2} dy="0.35em">
		<title>{band.source}</title>{fitText(band.source, scale.left - 2 * TITLE_X_PX, SMALL_CHAR_PX)}
	</text>
	<g clip-path="url(#{ids}-plot)">
		{#each band.unknown as u, i (i)}
			{@const x1 = scale.x(u.start)}
			{@const width = Math.max(scale.x(u.end) - x1, 0)}
			{@const visible = Math.min(x1 + width, scale.right) - Math.max(x1, scale.left)}
			<!-- A reason that does not fit in its span is written before it. -->
			{@const inside = u.reason.length * SMALL_CHAR_PX + 2 * TEXT_PAD_PX <= visible}
			<g data-unknown>
				<title>{u.reason}</title>
				<rect
					x={x1}
					y={band.y + INSET_PX}
					{width}
					height={SOURCE_ROW_PX - 2 * INSET_PX}
					style:fill="url(#{ids}-hatch)"
				/>
				{#if inside}
					<text
						class="reason"
						x={Math.max(x1, scale.left) + TEXT_PAD_PX}
						y={band.y + SOURCE_ROW_PX / 2}
						dy="0.35em">{u.reason}</text
					>
				{:else}
					<text
						class="reason"
						x={x1 - TEXT_PAD_PX}
						y={band.y + SOURCE_ROW_PX / 2}
						dy="0.35em"
						text-anchor="end"
						>{fitText(u.reason, x1 - scale.left - 2 * TEXT_PAD_PX, SMALL_CHAR_PX)}</text
					>
				{/if}
			</g>
		{/each}
	</g>
</g>

<style>
	.title {
		fill: var(--tl-muted);
		font-size: 10px;
	}
	.reason {
		fill: var(--tl-fg);
		font-size: 10px;
	}
</style>
