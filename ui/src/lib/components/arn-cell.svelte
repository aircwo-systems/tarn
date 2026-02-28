<script lang="ts">
	import { Tooltip } from '$lib/components/ui/tooltip';

	let {
		name,
		arn
	}: {
		name: string;
		arn: string;
	} = $props();
</script>

<div class="flex flex-col gap-0.5 max-w-[20rem]">
	<span class="font-medium text-text">{name}</span>
	<Tooltip text={arn}>
		<span class="font-mono text-[11px] text-text-faint truncate block cursor-default">{arn}</span>
	</Tooltip>
</div>
