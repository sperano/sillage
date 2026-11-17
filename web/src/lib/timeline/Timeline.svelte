<script lang="ts">
	import type { Attachment } from 'svelte/attachments';
	import { SvelteSet } from 'svelte/reactivity';
	import type { Delivery } from '../decodeApi';
	import { receivedTime } from '../deliveries';
	import type { Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import { layoutChart } from './chart';
	import CoverageBand from './CoverageBand.svelte';
	import { storySources } from './coverage';
	import { DELIVERY_MARKS_MAX_SPAN_MS, deliveryRow } from './deliveries';
	import { buildRows } from './lanes';
	import { TITLE_X_PX } from './layout';
	import { formatClock, formatDuration, type Zone } from './time';
	import TimelineAxis from './TimelineAxis.svelte';
	import TimelineRow from './TimelineRow.svelte';
	import TimelineToolbar from './TimelineToolbar.svelte';
	import {
		MIN_SPAN_MS,
		PAN_FRACTION,
		ZOOM_STEP,
		pan,
		scaleOf,
		storyTimes,
		windowAround,
		zoom,
		type TimeWindow
	} from './view';

	interface Props {
		story: Story;
		/** Shared with the event list and the detail panel. */
		selection: Selection;
		/** The zone of every time on the timeline. */
		zone?: Zone;
		/** The webhook deliveries sillage received for the story's root, on the Sillage lane. */
		deliveries?: readonly Delivery[];
	}

	let { story, selection, zone = $bindable('local'), deliveries = [] }: Props = $props();

	const LABEL_COLUMN_PX = 250;
	const RIGHT_PAD_PX = 24;
	const MIN_PLOT_PX = 240;
	const DEFAULT_WIDTH_PX = 960;
	/** Wheel travel, in pixels, that zooms by one step. */
	const WHEEL_STEP_PX = 300;
	/** Pointer travel, in pixels, under which a press is a click, not a drag. */
	const DRAG_THRESHOLD_PX = 3;
	/** PointerEvent.button of the main button. */
	const PRIMARY_BUTTON = 0;
	/** The section headings' baseline, below the top of their band. */
	const SECTION_BASELINE_PX = 15;
	/** The tile of the unknown hatching. */
	const HATCH_PX = 8;
	/** The tile of the waiting stripes and review-wait dots. */
	const FILL_TILE_PX = 6;

	const ids = $props.id();
	const phased = $derived(story.runs.some((r) => r.phases !== undefined));
	const domain = $derived(
		windowAround([...storyTimes(story), ...deliveries.map(receivedTime).filter(Number.isFinite)])
	);
	// Showing another story shows the whole of it again.
	let view = $derived(domain);
	const span = $derived(view.end - view.start);
	const domainSpan = $derived(domain.end - domain.start);
	// The deliveries' row draws them only in a narrow enough window.
	const storyRows = $derived(buildRows(story));
	const sillageRow = $derived(deliveryRow(deliveries, span));
	const rows = $derived(sillageRow ? [...storyRows, sillageRow] : storyRows);
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

	function zoomBy(factor: number, center = (view.start + view.end) / 2) {
		view = zoom(view, factor, center, domain);
	}

	function toggle(id: string) {
		if (expanded.has(id)) expanded.delete(id);
		else expanded.add(id);
	}

	// Ctrl or Cmd with the wheel zooms around the pointer, Shift with it
	// pans; a plain wheel scrolls the page. Svelte's wheel handlers are
	// passive, and these must prevent the page from scrolling.
	const wheel: Attachment<SVGSVGElement> = (svg) => {
		const onwheel = (e: WheelEvent) => {
			if (e.ctrlKey || e.metaKey) {
				e.preventDefault();
				const x = e.clientX - svg.getBoundingClientRect().left;
				zoomBy(ZOOM_STEP ** (e.deltaY / WHEEL_STEP_PX), scale.time(x));
			} else if (e.shiftKey) {
				e.preventDefault();
				const delta = e.deltaX || e.deltaY;
				view = pan(view, (delta / (scale.right - scale.left)) * span, domain);
			}
		};
		svg.addEventListener('wheel', onwheel, { passive: false });
		return () => svg.removeEventListener('wheel', onwheel);
	};

	// The plot's width, read at once, then a frame after each resize:
	// drawing at a new width changes the plot's height, which the browser
	// reports as a ResizeObserver loop when it happens in the frame that
	// observed the resize.
	const plotWidth: Attachment<HTMLElement> = (plot) => {
		width = plot.clientWidth;
		let frame = 0;
		const observer = new ResizeObserver(() => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => (width = plot.clientWidth));
		});
		observer.observe(plot);
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
		};
	};

	let drag: { x: number; window: TimeWindow; moved: boolean } | undefined;

	/** A primary press outside every mark starts dragging the window. */
	function onpointerdown(e: PointerEvent & { currentTarget: SVGSVGElement }) {
		if (e.button !== PRIMARY_BUTTON) return;
		if (e.target instanceof Element && e.target.closest('[role="button"]')) return;
		drag = { x: e.clientX, window: view, moved: false };
		e.currentTarget.setPointerCapture(e.pointerId);
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
		if (e.key === 'Escape') selection.select(undefined);
	}
</script>

<section class="timeline surface" aria-label="Timeline">
	<TimelineToolbar
		bind:zone
		{view}
		canZoomIn={span > Math.min(MIN_SPAN_MS, domainSpan)}
		canZoomOut={span < domainSpan}
		canPan={span < domainSpan}
		onzoom={(d) => zoomBy(d === 'in' ? 1 / ZOOM_STEP : ZOOM_STEP)}
		onpan={(d) =>
			(view = pan(view, (d === 'earlier' ? -PAN_FRACTION : PAN_FRACTION) * span, domain))}
		onreset={() => (view = domain)}
	/>
	<!-- Escape from any focused mark clears the selection; the marks are the
	     interactive elements, this only catches the key bubbling up. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="plot" {@attach plotWidth} {onkeydown}>
		<svg
			{@attach wheel}
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
				<pattern id="{ids}-hatch" width={HATCH_PX} height={HATCH_PX} patternUnits="userSpaceOnUse">
					<!-- One diagonal across the tile, and its continuations at two corners. -->
					<path
						class="hatch"
						d="M 0 {HATCH_PX} L {HATCH_PX} 0 M -1 1 L 1 -1 M {HATCH_PX - 1} {HATCH_PX +
							1} L {HATCH_PX + 1} {HATCH_PX - 1}"
					/>
				</pattern>
				<pattern
					id="{ids}-waiting"
					width={FILL_TILE_PX}
					height={FILL_TILE_PX}
					patternUnits="userSpaceOnUse"
				>
					<rect class="waiting-bg" width={FILL_TILE_PX} height={FILL_TILE_PX} />
					<path class="waiting-line" d="M 0 {FILL_TILE_PX / 2} H {FILL_TILE_PX}" />
				</pattern>
				<pattern
					id="{ids}-review"
					width={FILL_TILE_PX}
					height={FILL_TILE_PX}
					patternUnits="userSpaceOnUse"
				>
					<rect class="review-bg" width={FILL_TILE_PX} height={FILL_TILE_PX} />
					<circle class="review-dot" cx={FILL_TILE_PX / 2} cy={FILL_TILE_PX / 2} r="1" />
				</pattern>
			</defs>
			<TimelineAxis {scale} {zone} bottom={chart.height} />
			{#each chart.sources as band (band.source)}
				<CoverageBand {band} {scale} {ids} />
			{/each}
			{#each chart.sections as heading (heading.section)}
				<text class="section" x={TITLE_X_PX} y={heading.y + SECTION_BASELINE_PX}
					>{heading.title}</text
				>
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
	{#if deliveries.length > 0}
		<p class="legend" data-delivery-legend>
			Sillage lane (triangle): a webhook delivery sillage recorded receiving, drawn when the window
			spans {formatDuration(DELIVERY_MARKS_MAX_SPAN_MS)} or less. It is not evidence that Kelos received
			its own delivery, nor of what caused a change; a request sillage refused, or answered while not
			collecting, was not recorded, so a stretch without one does not mean none was sent.
		</p>
	{/if}
	{#if phased}
		<p class="legend" data-phase-legend>
			Under a run: the phases its pod reported. Dashed segment: both ends recorded; tick: one end
			recorded (chevron: in progress at the last read); two ticks: markers that do not pair. Grey:
			joined to the run by Task name and time, uncertain. A missing marker is not a missing phase.
		</p>
	{/if}
</section>

<style>
	.plot {
		overflow-x: auto;
	}
	svg {
		display: block;
		touch-action: pan-y;
		user-select: none;
	}
	.section {
		fill: var(--color-muted);
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
	}
	.hatch {
		stroke: var(--color-unknown);
		stroke-width: 1;
		opacity: 0.6;
	}
	.waiting-bg,
	.review-bg {
		fill: var(--color-surface);
	}
	.waiting-line {
		stroke: var(--color-waiting);
		stroke-width: 2;
	}
	.review-dot {
		fill: var(--color-review);
	}
	.legend {
		color: var(--color-muted);
		font-size: 0.8rem;
	}
</style>
