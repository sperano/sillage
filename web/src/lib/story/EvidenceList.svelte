<script lang="ts">
	import type { Evidence } from '../model';
	import { sourceName } from '../coverage';

	let { evidence }: { evidence: Evidence[] } = $props();
</script>

<ul class="evidence">
	{#each evidence as ev, i (i)}
		<li>
			{sourceName(ev.source)}
			{ev.instance}: <code>{ev.ref}</code>{#if ev.note}: {ev.note}{/if}
		</li>
	{/each}
</ul>
