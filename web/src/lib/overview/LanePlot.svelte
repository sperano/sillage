<!-- One lane of the overview over its window: hatched where the history is
     unknown, the lane's spans as bars (a dashed stub where one end is
     unknown, a chevron where it had not ended at the last read), its marks,
     and the cursor. Positions are fractions of the window, so the plot
     takes whatever width it is given. -->
<script lang="ts">
	import type { UnknownSpan } from '../timeline/coverage';
	import type { Lane } from './lanes';
	import { barText, describeMark, describeSpan, markGlyph, place, type Range } from './spans';

	interface Props {
		lane: Lane;
		range: Range;
		/** The cursor's time. */
		cursor: number;
		unknown: readonly UnknownSpan[];
		/** Whether bars show their text: only in an expanded row or group. */
		labelled: boolean;
		/** Moves the cursor to a time the reader clicked. */
		onpick: (t: number) => void;
	}

	let { lane, range, cursor, unknown, labelled, onpick }: Props = $props();

	const PERCENT = 100;
	const pct = (f: number) => `${f * PERCENT}%`;
	const at = (t: number) => (t - range.start) / (range.end - range.start);
	const inRange = (t: number) => t >= range.start && t < range.end;

	const bars = $derived(
		lane.spans.flatMap((span) => {
			const placement = place(span, range);
			return placement ? [{ span, placement }] : [];
		})
	);
	const marks = $derived(lane.marks.filter((m) => inRange(Date.parse(m.at))));
	const hatches = $derived(unknown.filter((u) => u.end > range.start && u.start < range.end));

	function pick(e: MouseEvent & { currentTarget: HTMLElement }) {
		const box = e.currentTarget.getBoundingClientRect();
		if (box.width > 0) {
			onpick(range.start + ((e.clientX - box.left) / box.width) * (range.end - range.start));
		}
	}
</script>

<!-- The plot is a pointer shortcut for the cursor's slider, which the
     keyboard reaches; the bars and marks carry their own names. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div class="plot" role="group" aria-label={lane.label} onclick={pick}>
	{#each hatches as u, i (i)}
		<span
			class="unknown"
			style:left={pct(Math.max(0, at(u.start)))}
			style:width={pct(Math.min(1, at(u.end)) - Math.max(0, at(u.start)))}
			title={u.reason}
		></span>
	{/each}
	{#each bars as { span, placement } (span.key)}
		{@const description = describeSpan(span)}
		<span
			class="bar {span.kind}"
			class:stub-start={placement.stub === 'start'}
			class:stub-end={placement.stub === 'end'}
			class:open={span.endState === 'open' && !placement.clippedEnd}
			class:failed={span.outcome === 'failed'}
			style:left={pct(placement.left)}
			style:width={placement.stub ? undefined : pct(placement.right - placement.left)}
			role="img"
			title={description}
			>{#if labelled && !placement.stub}<span class="text">{barText(span)}</span>{/if}</span
		>
	{/each}
	{#each marks as m (m.event)}
		<span
			class="mark"
			class:failed={m.kind === 'pr.closed'}
			style:left={pct(at(Date.parse(m.at)))}
			role="img"
			title={describeMark(m)}>{markGlyph(m.kind)}</span
		>
	{/each}
	{#if cursor >= range.start && cursor <= range.end}
		<span class="cursor" style:left={pct(at(cursor))} aria-hidden="true"></span>
	{/if}
	{#if lane.note}<span class="note">{lane.note}</span>{/if}
</div>

<style>
	.plot {
		position: relative;
		height: 1.6rem;
		cursor: crosshair;
	}
	.bar,
	.unknown,
	.mark,
	.cursor {
		position: absolute;
		box-sizing: border-box;
	}
	.unknown {
		top: 0;
		bottom: 0;
		background: repeating-linear-gradient(
			135deg,
			transparent 0 4px,
			color-mix(in srgb, var(--ov-unknown) 45%, transparent) 4px 5px
		);
	}
	.bar {
		top: 0.25rem;
		height: 1.1rem;
		min-width: 3px;
		border-radius: 3px;
		overflow: hidden;
		white-space: nowrap;
		font-size: 0.7rem;
		line-height: 1.1rem;
	}
	.text {
		padding: 0 0.3rem;
	}
	.waiting .text,
	.review .text {
		background: var(--ov-bg);
	}
	.running {
		background: var(--ov-running);
		color: var(--ov-on-running);
	}
	.running.failed {
		background: var(--ov-failure);
	}
	.waiting {
		background: repeating-linear-gradient(0deg, var(--ov-bg) 0 2px, var(--ov-waiting) 2px 4px);
		color: var(--ov-fg);
	}
	.review {
		background: radial-gradient(var(--ov-review) 1px, var(--ov-bg) 1.5px) 0 0 / 6px 6px;
		border: 1px dotted var(--ov-review);
		color: var(--ov-fg);
	}
	.pr {
		background: color-mix(in srgb, var(--ov-pr) 25%, var(--ov-bg));
		border: 1px solid var(--ov-pr);
		color: var(--ov-fg);
	}
	.open {
		border-right: none;
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}
	.open::after {
		content: '›';
		position: absolute;
		right: 1px;
		font-weight: 700;
	}
	.stub-start,
	.stub-end {
		width: 1.5rem;
		background: none;
		border: 1px dashed var(--ov-unknown);
	}
	.stub-start {
		transform: translateX(-100%);
	}
	.mark {
		top: 0.15rem;
		transform: translateX(-50%);
		font-size: 0.8rem;
		color: var(--ov-mark);
	}
	.mark.failed {
		color: var(--ov-failure);
	}
	.cursor {
		top: 0;
		bottom: 0;
		border-left: 2px dashed var(--ov-cursor);
	}
	.note {
		position: absolute;
		left: 0.3rem;
		font-size: 0.7rem;
		color: var(--ov-muted);
		line-height: 1.6rem;
	}
</style>
