<script lang="ts">
	import type { CheckRun, PullRequest } from '../model';
	import { checkLabel } from '../describe';
	import { checkURL, prURL } from '../links';
	import Bounds from './Bounds.svelte';

	let { pr, check, timeZone }: { pr: PullRequest; check: CheckRun; timeZone: string } = $props();
</script>

<h3>{checkLabel(pr, check)}</h3>
<dl>
	<dt>Pull request</dt>
	<dd>
		<a href={prURL(pr.key)} rel="noreferrer">{pr.key.repository}#{pr.key.number}</a>
		(branch <code>{pr.branch}</code>)
	</dd>
	<dt>Commit</dt>
	<dd><code>{check.sha}</code></dd>
	<dt>Conclusion</dt>
	<dd>{check.conclusion}</dd>
	<Bounds interval={check.interval} {timeZone} />
	<dt>Source record</dt>
	<dd><a href={checkURL(pr, check)} rel="noreferrer">GitHub check {check.id}</a></dd>
</dl>
