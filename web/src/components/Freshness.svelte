<!--
	How far each source instance has been read: its last successful read,
	the spans whose records are missing, and what the reads left unknown.
-->
<script lang="ts">
	import { SOURCE_NAMES, formatTime } from '../lib/format';
	import type { Coverage } from '../lib/model';

	let { coverage }: { coverage: readonly Coverage[] } = $props();
</script>

{#if coverage.length === 0}
	<p>No source has been read yet: nothing is known.</p>
{:else}
	<p>Nothing after a source's last successful read is known yet.</p>
	<ul class="sources">
		{#each coverage as c (`${c.source}/${c.instance}`)}
			<li>
				<h3>{SOURCE_NAMES[c.source]} <span class="instance">{c.instance}</span></h3>
				{#if c.lastSuccessfulRead}
					<p>
						Last read successfully at
						<time datetime={c.lastSuccessfulRead}>{formatTime(c.lastSuccessfulRead)}</time>.
					</p>
				{:else}
					<p>Never read successfully: nothing from it is known.</p>
				{/if}
				{#if c.gaps?.length}
					<p>Records are missing, so what happened is unknown:</p>
					<ul>
						{#each c.gaps as gap, i (i)}
							<li>
								from <time datetime={gap.start}>{formatTime(gap.start)}</time>
								to <time datetime={gap.end}>{formatTime(gap.end)}</time>: {gap.reason}
							</li>
						{/each}
					</ul>
				{/if}
				{#if c.notes?.length}
					<p>Also unknown:</p>
					<ul>
						{#each c.notes as note, i (i)}
							<li>{note}</li>
						{/each}
					</ul>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

<style>
	.sources {
		list-style: none;
		padding: 0;
	}
	h3 {
		font-size: 1rem;
		margin-bottom: 0.25rem;
	}
	.instance {
		font-weight: normal;
		color: #555;
	}
</style>
