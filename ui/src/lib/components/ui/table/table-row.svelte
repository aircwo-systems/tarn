<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<tr
	class={cn('border-b border-border transition-colors hover:bg-bg-surface/50', className)}
	{...rest}
>
	{@render children?.()}
</tr>
