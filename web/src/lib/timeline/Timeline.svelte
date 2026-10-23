<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import type { Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import { layoutChart } from './chart';
	import CoverageBand from './CoverageBand.svelte';
	import { storySources } from './coverage';
	import { buildRows } from './lanes';
	import { formatClock, type Zone } from './time';
	import TimelineAxis from './TimelineAxis.svelte';
	import TimelineRow from './TimelineRow.svelte';
	import TimelineToolbar from './TimelineToolbar.svelte';
	import {
		MIN_SPAN_MS,
		PAN_FRACTION,
		ZOOM_STEP,
		pan,
		scaleOf,
		storyDomain,
		zoom,
		type TimeWindow
	} from './view';

	interface Props {
		story: Story;
		/** Shared with the event list and the detail panel. */
		selection: Selection;
		/** The zone of every time on the timeline. */
		zone?: Zone;
	}

	let { story, selection, zone = $bindable('local') }: Props = $props();

	const LABEL_COLUMN_PX = 250;
	const RIGHT_PAD_PX = 24;
	const MIN_PLOT_PX = 240;
	const DEFAULT_WIDTH_PX = 960;
	/** Wheel travel, in pixels, that zooms by one step. */
	const WHEEL_STEP_PX = 300;
	/** Pointer travel, in pixels, under which a press is a click, not a drag. */
	const DRAG_THRESHOLD_PX = 3;

	const ids = $props.id();
	const rows = $derived(buildRows(story));
	const domain = $derived(storyDomain(story));
	// Showing another story shows the whole of it again.
	let view = $derived(domain);
	let width = $state(DEFAULT_WIDTH_PX);
	const expanded = new SvelteSet<string>();
	const scale = $derived(
		scaleOf(
			view,
			LABEL_COLUMN_PX,
			Math.max(width, LABEL_COLUMN_PX + MIN_PLOT_PX + RIGHT_PAD_PX) - RIGHT_PAD_PX
		)
	);
	const sources = $derived(
		storySources(
			story,
			rows.map((r) => r.sources)
		)
	);
	const chart = $derived(
		layoutChart({
			story,
			rows,
			sources,
			domain,
			scale,
			zone,
			expanded,
			timeText: (t) => formatClock(t, zone)
		})
	);
	const span = $derived(view.end - view.start);
	const domainSpan = $derived(domain.end - domain.start);

	function zoomBy(factor: number, center = (view.start + view.end) / 2) {
		view = zoom(view, factor, center, domain);
	}

	function toggle(id: string) {
		if (expanded.has(id)) expanded.delete(id);
		else expanded.add(id);
	}

	let svg: SVGSVGElement | undefined = $state();

	// Ctrl or Cmd with the wheel zooms around the pointer, Shift with it
	// pans; a plain wheel scrolls the page. Svelte's wheel handlers are
	// passive, and these must prevent the page from scrolling.
	$effect(() => {
		if (!svg) return;
		const onwheel = (e: WheelEvent) => {
			if (e.ctrlKey || e.metaKey) {
				e.preventDefault();
				const x = e.clientX - svg!.getBoundingClientRect().left;
				zoomBy(ZOOM_STEP ** (e.deltaY / WHEEL_STEP_PX), scale.time(x));
			} else if (e.shiftKey) {
				e.preventDefault();
				const delta = e.deltaX || e.deltaY;
				view = pan(view, (delta / (scale.right - scale.left)) * span, domain);
			}
		};
		svg.addEventListener('wheel', onwheel, { passive: false });
		return () => svg!.removeEventListener('wheel', onwheel);
	});

	let drag: { x: number; window: TimeWindow; moved: boolean } | undefined;

	function onpointerdown(e: PointerEvent) {
		if ((e.target as Element).closest('[role="button"]')) return;
		drag = { x: e.clientX, window: view, moved: false };
		svg?.setPointerCapture(e.pointerId);
	}

	function onpointermove(e: PointerEvent) {
		if (!drag) return;
		const dx = e.clientX - drag.x;
		if (Math.abs(dx) < DRAG_THRESHOLD_PX && !drag.moved) return;
		drag.moved = true;
		view = pan(drag.window, (-dx / (scale.right - scale.left)) * span, domain);
	}

	function onpointerup() {
		drag = undefined;
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') selection.clear();
	}
</script>

<section class="timeline" aria-label="Timeline">
	<TimelineToolbar
		bind:zone
		window={view}
		canZoomIn={span > Math.min(MIN_SPAN_MS, domainSpan)}
		canZoomOut={span < domainSpan}
		canPan={span < domainSpan}
		onzoom={(d) => zoomBy(d === 'in' ? 1 / ZOOM_STEP : ZOOM_STEP)}
		onpan={(d) =>
			(view = pan(view, (d === 'earlier' ? -PAN_FRACTION : PAN_FRACTION) * span, domain))}
		onreset={() => (view = domain)}
	/>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="plot" bind:clientWidth={width} {onkeydown}>
		<svg
			bind:this={svg}
			width={scale.right + RIGHT_PAD_PX}
			height={chart.height}
			role="group"
			aria-label="Lanes of the story over time"
			{onpointerdown}
			{onpointermove}
			{onpointerup}
			onpointercancel={onpointerup}
		>
			<defs>
				<clipPath id="{ids}-plot">
					<rect x={scale.left} y="0" width={scale.right - scale.left} height={chart.height} />
				</clipPath>
				<pattern id="{ids}-hatch" width="8" height="8" patternUnits="userSpaceOnUse">
					<path class="hatch" d="M -2 2 L 2 -2 M 0 8 L 8 0 M 6 10 L 10 6" />
				</pattern>
				<pattern id="{ids}-waiting" width="6" height="6" patternUnits="userSpaceOnUse">
					<rect class="waiting-bg" width="6" height="6" />
					<path class="waiting-line" d="M 0 3 H 6" />
				</pattern>
				<pattern id="{ids}-review" width="6" height="6" patternUnits="userSpaceOnUse">
					<rect class="review-bg" width="6" height="6" />
					<circle class="review-dot" cx="3" cy="3" r="1" />
				</pattern>
			</defs>
			<TimelineAxis {scale} {zone} bottom={chart.height} />
			{#each chart.sources as band (band.source)}
				<CoverageBand {band} {scale} {ids} />
			{/each}
			{#each chart.sections as heading (heading.section)}
				<text class="section" x="12" y={heading.y + 15}>{heading.title}</text>
			{/each}
			{#each chart.rows as layout (layout.row.id)}
				<TimelineRow {layout} {scale} {zone} {selection} {ids} ontoggle={toggle} />
			{/each}
		</svg>
	</div>
	<p class="legend">
		Dot: event (ring: state observed by polling, time unknown). Bar: interval with both ends
		recorded; chevron: not ended at the last read; dashed stub: one end unknown. Hatched: no records
		known. Ctrl + wheel zooms, Shift + wheel or drag pans.
	</p>
</section>

<style>
	.timeline {
		--tl-bg: #ffffff;
		--tl-fg: #1d2733;
		--tl-muted: #5b6b7c;
		--tl-grid: #d9e0e8;
		--tl-event: #1f5f9e;
		--tl-observed: #1f5f9e;
		--tl-success: #1b7a4c;
		--tl-failure: #b3261e;
		--tl-merged: #6f42c1;
		--tl-running: #2a7f86;
		--tl-waiting: #a86b00;
		--tl-review: #6f42c1;
		--tl-check: #1f5f9e;
		--tl-unknown: #8a96a3;
		--tl-selected: #d97706;
		--tl-focus: #0b57d0;
		color: var(--tl-fg);
		background: var(--tl-bg);
		font-family: system-ui, sans-serif;
	}
	@media (prefers-color-scheme: dark) {
		.timeline {
			--tl-bg: #10161d;
			--tl-fg: #e3e9ef;
			--tl-muted: #9aa8b6;
			--tl-grid: #2b3743;
			--tl-event: #7ab3ef;
			--tl-observed: #7ab3ef;
			--tl-success: #5cc98f;
			--tl-failure: #f2867e;
			--tl-merged: #b99af0;
			--tl-running: #5cc2ca;
			--tl-waiting: #e0a84a;
			--tl-review: #b99af0;
			--tl-check: #7ab3ef;
			--tl-unknown: #6b7884;
			--tl-selected: #f59e0b;
			--tl-focus: #8ab4f8;
		}
	}
	.plot {
		overflow-x: auto;
	}
	svg {
		display: block;
		touch-action: pan-y;
		user-select: none;
	}
	.section {
		fill: var(--tl-muted);
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
	}
	.hatch {
		stroke: var(--tl-unknown);
		stroke-width: 1;
		opacity: 0.6;
	}
	.waiting-bg,
	.review-bg {
		fill: var(--tl-bg);
	}
	.waiting-line {
		stroke: var(--tl-waiting);
		stroke-width: 2;
	}
	.review-dot {
		fill: var(--tl-review);
	}
	.legend {
		color: var(--tl-muted);
		font-size: 0.8rem;
	}
</style>
