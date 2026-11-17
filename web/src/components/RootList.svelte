<!-- Stored roots a search or a link matched, each a link to its story. -->
<script lang="ts">
	import type { RootSummary } from '../lib/decodeApi';
	import { formatTime } from '../lib/format';
	import { rootLabel } from '../lib/roots';
	import { rootHref } from '../lib/routes';

	let { roots, label }: { roots: readonly RootSummary[]; label: string } = $props();
</script>

<ul aria-label={label}>
	{#each roots as hit (rootHref(hit.root))}
		<li>
			<a href={rootHref(hit.root)}>{hit.title ?? rootLabel(hit.root)}</a>
			{#if hit.title}<span class="key">{rootLabel(hit.root)}</span>{/if}
			{#if hit.lastActivity}
				<span class="activity">
					last activity
					<time datetime={hit.lastActivity}>{formatTime(hit.lastActivity)}</time>
				</span>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.key,
	.activity {
		color: var(--color-muted);
		margin-left: 0.5rem;
	}
</style>
