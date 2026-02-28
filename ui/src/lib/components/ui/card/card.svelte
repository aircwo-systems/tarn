<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<div class={cn('rounded-lg border border-border bg-card text-card-foreground', className)} {...rest}>
	{@render children?.()}
</div>
