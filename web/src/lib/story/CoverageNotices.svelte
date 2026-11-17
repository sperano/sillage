<script lang="ts">
	import type { Story } from '../model';
	import { sourceName, uncoveredSources } from '../coverage';
	import Time from './Time.svelte';

	let { story, timeZone }: { story: Story; timeZone: string } = $props();

	const uncovered = $derived(uncoveredSources(story));
</script>

<section class="coverage surface" aria-labelledby="coverage-heading">
	<h2 id="coverage-heading">Coverage</h2>
	{#if story.coverage.length === 0 && uncovered.length === 0}
		<p>No source has been read yet: nothing is known.</p>
	{:else}
		<ul>
			{#each story.coverage as c (`${c.source}/${c.instance}`)}
				<li data-source={c.source}>
					<strong>{sourceName(c.source)} {c.instance}</strong>:
					{#if c.lastSuccessfulRead}
						last successful read <Time at={c.lastSuccessfulRead} {timeZone} />; unknown after
						<Time at={c.lastSuccessfulRead} {timeZone} />.
					{:else}
						never read successfully: everything it holds is unknown.
					{/if}
					{#if c.gaps?.length || c.notes?.length}
						<ul>
							{#each c.gaps ?? [] as g, i (i)}
								<li class="gap">
									Unknown from <Time at={g.start} {timeZone} /> to <Time at={g.end} {timeZone} />:
									{g.reason}
								</li>
							{/each}
							{#each c.notes ?? [] as note, i (i)}
								<li class="note">{note}</li>
							{/each}
						</ul>
					{/if}
				</li>
			{/each}
			{#each uncovered as s (`${s.source}/${s.instance}`)}
				<li data-source={s.source}>
					<strong>{sourceName(s.source)} {s.instance}</strong>: no coverage record; how far it was
					read is unknown.
				</li>
			{/each}
		</ul>
	{/if}
</section>
