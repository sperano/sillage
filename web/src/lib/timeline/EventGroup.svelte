<script lang="ts">
	import type { Selection } from '../selection.svelte';
	import EventGlyph from './EventGlyph.svelte';
	import { activation } from './keys';
	import { LINE_PX, type Point } from './layout';
	import { FOCUS_RING_R_PX } from './marks';
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
	/** The keyboard focus ring's distance outside the badge. */
	const FOCUS_GAP_PX = 4;

	const first = $derived(members[0]!);
	const last = $derived(members[members.length - 1]!);
	const noun = $derived(
		members.every((m) => m.selected.kind === 'delivery') ? 'deliveries' : 'events'
	);
	const name = $derived(
		`${members.length} ${noun} from ${formatFull(first.at, zone)} to ${formatFull(last.at, zone)}`
	);
	const toggle = () => ontoggle(id);
</script>

<g class="group" data-group={id}>
	<g
		class="badge"
		role="button"
		tabindex="0"
		aria-label={name}
		aria-expanded={expanded}
		onclick={toggle}
		{...activation(toggle)}
	>
		<title>{name}</title>
		<circle class="focus" cx={x} cy={y} r={BADGE_R + FOCUS_GAP_PX} />
		<circle class="count" cx={x} cy={y} r={BADGE_R} />
		<text {x} {y} dy="0.35em">{members.length}</text>
	</g>
	{#if expanded}
		{#each members as m, i (m.key)}
			{@const select = () => selection.select(m.selected)}
			{@const selected = selection.isSelected(m.selected)}
			{@const lineY = listY + i * LINE_PX}
			{@const name = m.name(zone)}
			<g
				class="member"
				role="button"
				tabindex="0"
				aria-label={name}
				aria-pressed={selected}
				data-event={m.selected.kind === 'event' ? m.key : undefined}
				data-delivery={m.selected.kind === 'delivery' ? m.key : undefined}
				onclick={select}
				{...activation(select)}
			>
				<title>{name}</title>
				<line class="lead" x1={m.x} y1={y} x2={x + LIST_INDENT_PX} y2={lineY} />
				<circle class="focus" cx={x + LIST_INDENT_PX} cy={lineY} r={FOCUS_RING_R_PX} />
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
	.badge .count {
		fill: var(--color-surface);
		stroke: var(--color-event);
		stroke-width: 2;
	}
	.focus {
		fill: none;
		stroke: var(--color-focus);
		stroke-width: 2;
		stroke-dasharray: 3 2;
		opacity: 0;
	}
	.badge:focus-visible .focus,
	.member:focus-visible .focus {
		opacity: 1;
	}
	.badge text {
		fill: var(--color-fg);
		font-size: 10px;
		font-weight: 700;
		text-anchor: middle;
	}
	.member text {
		fill: var(--color-fg);
		font-size: 10px;
	}
	.lead {
		stroke: var(--color-grid);
	}
</style>
