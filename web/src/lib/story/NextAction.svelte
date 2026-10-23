<script lang="ts">
	import type { Story } from '../model';
	import { nextActions } from '../nextAction';
	import Time from './Time.svelte';

	let { story, timeZone }: { story: Story; timeZone: string } = $props();

	const actions = $derived(nextActions(story));
</script>

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
						requested
						<Time at={a.requested.sourceTime ?? a.requested.observedAt} {timeZone} />
					{:else}
						Waiting for an agent: {a.request.details?.command ?? 'request'} by
						{a.request.actor.login ?? 'an unknown account'}
						<Time at={a.request.sourceTime ?? a.request.observedAt} {timeZone} />, no start
						announced
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>
