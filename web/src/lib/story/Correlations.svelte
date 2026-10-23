<script lang="ts">
	import type { Ref, Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import { findItem, type TimelineItem } from '../timeline';
	import { linksOf } from '../correlations';
	import { refLabel } from '../describe';
	import EvidenceList from './EvidenceList.svelte';

	let {
		story,
		items,
		selection,
		subject
	}: { story: Story; items: TimelineItem[]; selection: Selection; subject: Ref } = $props();

	const links = $derived(linksOf(story, subject));

	function selectable(ref: Ref): TimelineItem | undefined {
		return ref.kind === 'event' || ref.kind === 'run'
			? findItem(items, { kind: ref.kind, id: ref.id })
			: undefined;
	}
</script>

{#snippet other(ref: Ref)}
	{@const item = selectable(ref)}
	{#if item}
		<button type="button" class="link" onclick={() => selection.select(item.ref)}
			>{refLabel(story, ref)}</button
		>
	{:else}
		{refLabel(story, ref)}
	{/if}
{/snippet}

<h4>Correlations</h4>
{#if links.length === 0}
	<p>None: no other record is linked to this one.</p>
{:else}
	<ul class="correlations">
		{#each links as l, i (i)}
			<li>
				{@render other(l.other)}
				<span class="confidence" data-confidence={l.correlation.confidence}>
					{#if l.correlation.confidence === 'identifier'}
						confidence: identifier (an identifier both records carry)
					{:else}
						confidence: heuristic (uncertain; not a causal link)
					{/if}
				</span>
				{#if l.correlation.ambiguous}
					<div class="ambiguous">
						Ambiguous: {l.candidates.length} candidates fit the evidence, none was chosen:
						<ul>
							{#each l.candidates as c (c.kind + c.id)}
								<li>{@render other(c)}</li>
							{/each}
						</ul>
					</div>
				{/if}
				<EvidenceList evidence={l.correlation.evidence} />
			</li>
		{/each}
	</ul>
{/if}

<style>
	.link {
		border: none;
		background: none;
		padding: 0;
		color: LinkText;
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}

	.confidence {
		display: block;
		font-size: 0.875em;
	}

	.ambiguous {
		font-weight: 600;
	}
</style>
