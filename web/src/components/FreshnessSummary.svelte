<!--
	How recently each source instance was read, across the stored tasks: the
	latest and the stalest successful read, and how many were never read;
	and which sources are off, never read, and why. Missing records are each
	task's own.
-->
<script lang="ts">
	import { compareTimes } from '../lib/check';
	import type { OffSource, SourceFreshness } from '../lib/decodeApi';
	import { SOURCE_NAMES, count, formatTime } from '../lib/format';

	let { sources, off }: { sources: readonly SourceFreshness[]; off: readonly OffSource[] } =
		$props();

	/** The off record of a source instance read before, if it is off now. */
	function offOf(s: SourceFreshness): OffSource | undefined {
		return off.find((o) => o.source === s.source && o.instance === s.instance);
	}

	/** The off sources no stored task's coverage names. */
	const offOnly = $derived(off.filter((o) => !sources.some((s) => offOf(s) === o)));
</script>

{#snippet offNotice(o: OffSource)}
	<p class="off">
		Off: sillage does not read it ({o.reason}). Nothing from it after its last read is known.
	</p>
{/snippet}

{#if sources.length === 0}
	<p>No source has been read yet: nothing is known.</p>
{:else}
	<p>
		Nothing after a source's last successful read is known yet. Where records are missing is each
		task's own: its story shows it.
	</p>
{/if}
{#if sources.length > 0 || offOnly.length > 0}
	<ul class="sources">
		{#each offOnly as o (`${o.source}/${o.instance ?? ''}`)}
			<li>
				<h3>{SOURCE_NAMES[o.source]} <span class="instance">{o.instance ?? ''}</span></h3>
				{@render offNotice(o)}
			</li>
		{/each}
		{#each sources as s (`${s.source}/${s.instance}`)}
			{@const sOff = offOf(s)}
			<li>
				<h3>{SOURCE_NAMES[s.source]} <span class="instance">{s.instance}</span></h3>
				{#if sOff}
					{@render offNotice(sOff)}
				{/if}
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
	.off {
		font-weight: bold;
	}
	h3 {
		font-size: 1rem;
		margin-bottom: 0.25rem;
	}
	.instance {
		font-weight: normal;
		color: var(--app-muted);
	}
</style>
