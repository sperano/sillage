<script lang="ts">
	import { tick } from 'svelte';
	import type { Ref, Story } from '../model';
	import type { Selection } from '../selection.svelte';
	import { findItem, type TimelineItem } from '../timeline';
	import { linksOf } from '../correlations';
	import { refLabel } from '../describe';
	import EvidenceList from './EvidenceList.svelte';
	import LinkButton from './LinkButton.svelte';

	let {
		story,
		items,
		selection,
		subject
	}: { story: Story; items: TimelineItem[]; selection: Selection; subject: Ref } = $props();

	const links = $derived(linksOf(story, subject));

	/**
	 * Selects the record and moves the focus to the heading of its detail,
	 * since the detail that held the button is replaced.
	 */
	async function choose(event: MouseEvent, item: TimelineItem) {
		const panel = (event.currentTarget as HTMLElement).closest('section');
		selection.select(item.ref);
		await tick();
		panel?.querySelector<HTMLElement>('h3')?.focus();
	}

	function refKey(r: Ref): string {
		return `${r.kind}:${r.id}`;
	}

	function selectable(ref: Ref): TimelineItem | undefined {
		return ref.kind === 'event' || ref.kind === 'run'
			? findItem(items, { kind: ref.kind, id: ref.id })
			: undefined;
	}
</script>

{#snippet other(ref: Ref)}
	{@const item = selectable(ref)}
	{#if item}
		<LinkButton onclick={(e) => choose(e, item)}>{refLabel(story, ref)}</LinkButton>
	{:else}
		{refLabel(story, ref)}
	{/if}
{/snippet}

<h4>Correlations</h4>
{#if links.length === 0}
	<p>None: no other record is linked to this one.</p>
{:else}
	<ul class="correlations">
		{#each links as l (`${refKey(l.correlation.from)} ${refKey(l.correlation.to)}`)}
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
						{#each l.candidateSets as set, i (i)}
							Ambiguous: {set.length} candidates fit the evidence, none was chosen:
							<ul>
								{#each set as c (refKey(c))}
									<li>{@render other(c)}</li>
								{/each}
							</ul>
						{:else}
							Ambiguous: one of several candidates, none chosen; no other candidate is linked
							ambiguously in this story (one may be linked by stronger evidence).
						{/each}
					</div>
				{/if}
				<EvidenceList evidence={l.correlation.evidence} />
			</li>
		{/each}
	</ul>
{/if}

<style>
	.confidence {
		display: block;
		font-size: 0.875em;
	}

	.ambiguous {
		font-weight: 600;
	}
</style>
