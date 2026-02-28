<script lang="ts">
	import { cn } from '$lib/utils';
	import { buttonVariants, type ButtonVariant, type ButtonSize } from './index.js';

	let {
		class: className,
		variant = 'default',
		size = 'default',
		children,
		...rest
	}: {
		class?: string;
		variant?: ButtonVariant;
		size?: ButtonSize;
		children?: import('svelte').Snippet;
		[key: string]: any;
	} = $props();
</script>

<button class={cn(buttonVariants({ variant, size }), className)} {...rest}>
	{@render children?.()}
</button>
