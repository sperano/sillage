<script lang="ts">
	import type { SourceLink } from '../links';

	let { link }: { link: SourceLink | undefined } = $props();
</script>

{#if link}
	<a href={link.href} rel="noreferrer">{link.label}</a>
{:else}
	none: its keys locate no record to link
{/if}
