<script lang="ts">
	import type { Event, Story } from '../model';
	import { nextActions } from '../nextAction';
	import Time from './Time.svelte';

	let { story, timeZone }: { story: Story; timeZone: string } = $props();

	const actions = $derived(nextActions(story));
</script>

{#snippet when(e: Event)}
	{#if e.sourceTime}
		<Time at={e.sourceTime} {timeZone} />
	{:else}
		(seen <Time at={e.observedAt} {timeZone} />, no source time)
	{/if}
{/snippet}

<section class="next-action" aria-labelledby="next-action-heading">
	<h2 id="next-action-heading">Next action</h2>
	{#if actions.length === 0}
		<p>Unknown: no pending review request and no unanswered agent request in the evidence.</p>
	{:else}
		<ul>
			{#each actions as a, i (i)}
				<li>
					{#if a.kind === 'review'}
						Review by {a.reviewer}{a.team ? ' (team)' : ''} on {a.pr.repository}#{a.pr.number},
						requested {@render when(a.requested)}
					{:else}
						Waiting for an agent: {a.request.details?.command ?? 'request'} by
						{a.request.actor.login ?? 'an unknown account'}
						{@render when(a.request)}, no start announced
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>
