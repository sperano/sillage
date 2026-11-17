<!-- The runs of one agent profile: a lane per run, so that two of its runs at
     the same time stay two lanes. Each lane names its task and links to its
     story. -->
<script lang="ts">
	import { count } from '../format';
	import { rootHref } from '../routes';
	import type { Zone } from '../timeline/time';
	import { NO_EXECUTOR, type ExecutorGroup } from './lanes';
	import LanePlot from './LanePlot.svelte';
	import type { Range } from './spans';
	import { rowUnknownSpans } from './unknown';

	interface Props {
		group: ExecutorGroup;
		range: Range;
		cursor: number;
		zone: Zone;
		onpick: (t: number) => void;
	}

	let { group, range, cursor, zone, onpick }: Props = $props();

	const name = $derived(group.executor ?? NO_EXECUTOR);
</script>

<li class="group">
	<div class="name">
		<strong>{name}</strong>
		<span class="sub">{count(group.lanes.length, 'run')}</span>
	</div>
	<ul class="lanes" aria-label="Runs of {name}">
		{#each group.lanes as lane (lane.key)}
			<li>
				{#if lane.owner}
					<a class="lane-label" href={rootHref(lane.owner.root)}>{lane.label}</a>
				{:else}
					<span class="lane-label">{lane.label}</span>
				{/if}
				<LanePlot
					{lane}
					{range}
					{cursor}
					{zone}
					unknown={lane.owner ? rowUnknownSpans(lane.owner, range, zone) : []}
					labelled
					{onpick}
				/>
			</li>
		{/each}
	</ul>
	<span aria-hidden="true"></span>
</li>

<style>
	.group {
		display: grid;
		grid-template-columns: var(--ov-name-width) 1fr var(--ov-state-width);
		gap: 0.5rem;
		padding: 0.4rem 0;
		border-top: 1px solid var(--color-grid);
	}
	.name {
		display: flex;
		flex-direction: column;
		font-size: 0.85rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.sub,
	.lane-label {
		color: var(--color-muted);
		font-size: 0.7rem;
	}
	.lane-label {
		overflow-wrap: anywhere;
	}
	.lanes {
		list-style: none;
		margin: 0;
		padding: 0;
		min-width: 0;
	}
</style>
