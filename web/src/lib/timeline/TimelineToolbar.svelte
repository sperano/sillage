<script lang="ts">
	import { formatDate, formatMinute, zoneName, type Zone } from './time';
	import TimeZoneSelect from './TimeZoneSelect.svelte';
	import type { TimeWindow } from './view';

	interface Props {
		zone: Zone;
		view: TimeWindow;
		canZoomIn: boolean;
		canZoomOut: boolean;
		canPan: boolean;
		onzoom: (direction: 'in' | 'out') => void;
		onpan: (direction: 'earlier' | 'later') => void;
		onreset: () => void;
	}

	let {
		zone = $bindable(),
		view,
		canZoomIn,
		canZoomOut,
		canPan,
		onzoom,
		onpan,
		onreset
	}: Props = $props();

	const range = $derived(
		`${formatDate(view.start, zone)} ${formatMinute(view.start, zone)} – ` +
			`${formatDate(view.end, zone)} ${formatMinute(view.end, zone)} ${zoneName(zone)}`
	);
</script>

<div class="toolbar">
	<TimeZoneSelect bind:zone />
	<div class="buttons" role="group" aria-label="Zoom and pan">
		<button type="button" onclick={onreset}>Whole story</button>
		<button type="button" onclick={() => onzoom('in')} disabled={!canZoomIn}>Zoom in</button>
		<button type="button" onclick={() => onzoom('out')} disabled={!canZoomOut}>Zoom out</button>
		<button type="button" onclick={() => onpan('earlier')} disabled={!canPan}>Earlier</button>
		<button type="button" onclick={() => onpan('later')} disabled={!canPan}>Later</button>
	</div>
	<output class="range">{range}</output>
</div>

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem 1rem;
		align-items: center;
		padding: 0.5rem 0;
		font-size: 0.85rem;
	}
	.buttons {
		display: flex;
		gap: 0.25rem;
	}
	button {
		font: inherit;
		color: var(--tl-fg);
		background: var(--tl-bg);
		border: 1px solid var(--tl-grid);
		border-radius: 4px;
		padding: 0.2rem 0.5rem;
	}
	button:disabled {
		color: var(--tl-muted);
	}
	.range {
		color: var(--tl-muted);
	}
</style>
