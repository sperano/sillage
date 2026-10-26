<!--
	How recently each source instance was read, across the stored tasks: the
	latest and the stalest successful read, and how many were never read.
	Missing records are each task's own.
-->
<script lang="ts">
	import { compareTimes } from '../lib/check';
	import type { SourceFreshness } from '../lib/decodeApi';
	import { SOURCE_NAMES, count, formatTime } from '../lib/format';

	let { sources }: { sources: readonly SourceFreshness[] } = $props();
</script>

{#if sources.length === 0}
	<p>No source has been read yet: nothing is known.</p>
{:else}
	<p>
		Nothing after a source's last successful read is known yet. Where records are missing is each
		task's own: its story shows it.
	</p>
	<ul class="sources">
		{#each sources as s (`${s.source}/${s.instance}`)}
			<li>
				<h3>{SOURCE_NAMES[s.source]} <span class="instance">{s.instance}</span></h3>
				<p>Read for {count(s.roots, 'task')}.</p>
				{#if s.newestRead && s.oldestRead}
					<p>
						Last read successfully at
						<time datetime={s.newestRead}>{formatTime(s.newestRead)}</time
						>{#if compareTimes(s.oldestRead, s.newestRead) < 0}; the least recently read of them at
							<time datetime={s.oldestRead}>{formatTime(s.oldestRead)}</time>{/if}.
					</p>
				{/if}
				{#if s.neverRead === s.roots}
					<p>Never read successfully: nothing from it is known.</p>
				{:else if s.neverRead > 0}
					<p>
						{count(s.neverRead, 'task')} of them never read successfully: nothing from it is known for
						those.
					</p>
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
