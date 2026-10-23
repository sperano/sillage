<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import EventGlyph from './EventGlyph.svelte';
	import { onActivate } from './keys';
	import { LINE_PX, type Point } from './layout';
	import { formatClock, formatFull, type Zone } from './time';

	interface Props {
		id: string;
		x: number;
		members: Point[];
		expanded: boolean;
		y: number;
		/** Where an expanded group's list starts. */
		listY: number;
		zone: Zone;
		selection: Selection;
		ontoggle: (id: string) => void;
	}

	let { id, x, members, expanded, y, listY, zone, selection, ontoggle }: Props = $props();

	/** The count badge's radius. */
	const BADGE_R = 9;
	/** How far right of the group the expanded list starts. */
	const LIST_INDENT_PX = 8;

	const first = $derived(members[0]!);
	const last = $derived(members[members.length - 1]!);
	const name = $derived(
		`${members.length} events from ${formatFull(first.at, zone)} to ${formatFull(last.at, zone)}`
	);
	const toggle = () => ontoggle(id);
</script>

<g class="group" data-group={id}>
	<g
		class="badge"
		role="button"
		tabindex="0"
		aria-label="{name}; {expanded ? 'collapse' : 'expand'}"
		aria-expanded={expanded}
		onclick={toggle}
		onkeydown={onActivate(toggle)}
	>
		<title>{name}</title>
		<circle cx={x} cy={y} r={BADGE_R} />
		<text {x} {y} dy="0.35em">{members.length}</text>
	</g>
	{#if expanded}
		{#each members as m, i (m.key)}
			{@const select = () => selection.select(m.selected)}
			{@const selected = selection.is(m.selected)}
			{@const lineY = listY + i * LINE_PX}
			<g
				class="member"
				role="button"
				tabindex="0"
				aria-label="{m.label}, {formatFull(m.at, zone)}"
				aria-pressed={selected}
				data-event={m.key}
				onclick={select}
				onkeydown={onActivate(select)}
			>
				<title>{m.label}, {formatFull(m.at, zone)}</title>
				<line class="lead" x1={m.x} y1={y} x2={x + LIST_INDENT_PX} y2={lineY} />
				<EventGlyph mark={m.mark} x={x + LIST_INDENT_PX} y={lineY} {selected} />
				<text x={x + LIST_INDENT_PX * 2} y={lineY} dy="0.35em"
					>{m.label} · {formatClock(m.at, zone)}</text
				>
			</g>
		{/each}
	{/if}
</g>

<style>
	.badge,
	.member {
		cursor: pointer;
		outline: none;
	}
	.badge circle {
		fill: var(--tl-bg);
		stroke: var(--tl-event);
		stroke-width: 2;
	}
	.badge:focus-visible circle,
	.member:focus-visible text {
		stroke: var(--tl-focus);
	}
	.badge text {
		fill: var(--tl-fg);
		font-size: 10px;
		font-weight: 700;
		text-anchor: middle;
	}
	.member text {
		fill: var(--tl-fg);
		font-size: 10px;
	}
	.lead {
		stroke: var(--tl-grid);
	}
</style>
