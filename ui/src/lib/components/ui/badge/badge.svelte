<script lang="ts">
	import { cn } from '$lib/utils';
	import { badgeVariants, type BadgeVariant } from './index.js';

	let {
		class: className,
		variant = 'default',
		children,
		...rest
	}: { class?: string; variant?: BadgeVariant; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<span class={cn(badgeVariants({ variant }), className)} {...rest}>
	{@render children?.()}
</span>
