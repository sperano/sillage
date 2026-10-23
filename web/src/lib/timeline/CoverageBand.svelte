<script lang="ts">
	import { SOURCE_ROW_PX, type SourceBand } from './chart';
	import { reasonTexts } from './coverage';
	import { fitText, SMALL_CHAR_PX, TITLE_X_PX } from './layout';
	import type { Scale } from './view';

	interface Props {
		band: SourceBand;
		scale: Scale;
		/** Prefix of the ids of the chart's patterns and clip path. */
		ids: string;
	}

	let { band, scale, ids }: Props = $props();

	const INSET_PX = 3;

	const reasons = $derived(reasonTexts(band.unknown, scale));
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
			{@const reason = reasons[i]}
			<g data-unknown role="img" aria-label={u.reason}>
				<title>{u.reason}</title>
				<rect
					x={x1}
					y={band.y + INSET_PX}
					{width}
					height={SOURCE_ROW_PX - 2 * INSET_PX}
					style:fill="url(#{ids}-hatch)"
				/>
				<!-- Written in the span, before it, or not at all: see reasonTexts. -->
				{#if reason}
					<text
						class="reason"
						x={reason.x}
						y={band.y + SOURCE_ROW_PX / 2}
						dy="0.35em"
						text-anchor={reason.anchor}>{reason.text}</text
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
