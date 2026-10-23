<script lang="ts">
	import type { CheckRun, PullRequest } from '../model';
	import { checkLabel } from '../describe';
	import { checkURL, prURL } from '../links';
	import Bounds from './Bounds.svelte';
	import SourceLinkView from './SourceLinkView.svelte';

	let { pr, check, timeZone }: { pr: PullRequest; check: CheckRun; timeZone: string } = $props();

	const prHref = $derived(prURL(pr.key));
	const checkHref = $derived(checkURL(pr, check));
	const checkLink = $derived(
		checkHref === undefined ? undefined : { href: checkHref, label: `GitHub check ${check.id}` }
	);
</script>

<h3 tabindex="-1">{checkLabel(pr, check)}</h3>
<dl>
	<dt>Pull request</dt>
	<dd>
		{#if prHref}
			<a href={prHref} rel="noreferrer">{pr.key.repository}#{pr.key.number}</a>
		{:else}
			{pr.key.repository}#{pr.key.number}
		{/if}
		(branch <code>{pr.branch}</code>)
	</dd>
	<dt>Commit</dt>
	<dd><code>{check.sha}</code></dd>
	<dt>Conclusion</dt>
	<dd>{check.conclusion}</dd>
	<Bounds interval={check.interval} {timeZone} />
	<dt>Source record</dt>
	<dd>
		<SourceLinkView link={checkLink} />
	</dd>
</dl>
