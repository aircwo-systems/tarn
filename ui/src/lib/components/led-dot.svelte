<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		color = 'gray',
		size = 'sm',
		class: className
	}: {
		color?: 'green' | 'amber' | 'red' | 'gray';
		size?: 'sm' | 'md';
		class?: string;
	} = $props();

	const colors = {
		green: 'bg-accent shadow-[0_0_6px_var(--color-accent)]',
		amber: 'bg-amber shadow-[0_0_6px_var(--color-amber)]',
		red: 'bg-red shadow-[0_0_6px_var(--color-red)]',
		gray: 'bg-text-faint'
	};

	const sizes = {
		sm: 'h-1.5 w-1.5',
		md: 'h-2 w-2'
	};
</script>

<span
	class={cn(
		'inline-block rounded-full shrink-0',
		sizes[size],
		colors[color],
		color !== 'gray' && color !== 'red' && 'animate-led-pulse',
		className
	)}
	aria-hidden="true"
></span>

<style>
	@keyframes led-pulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.55; }
	}
	:global(.animate-led-pulse) {
		animation: led-pulse 2s ease-in-out infinite;
	}
	@media (prefers-reduced-motion: reduce) {
		:global(.animate-led-pulse) {
			animation: none;
		}
	}
</style>
