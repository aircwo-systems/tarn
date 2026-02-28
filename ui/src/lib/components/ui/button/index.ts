import { tv, type VariantProps } from 'tailwind-variants';

export const buttonVariants = tv({
	base: 'inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50',
	variants: {
		variant: {
			default: 'bg-accent text-bg hover:bg-accent/90',
			secondary: 'border border-border bg-bg-surface text-text hover:bg-bg-overlay',
			ghost: 'hover:bg-bg-surface text-text-muted hover:text-text',
			outline: 'border border-border bg-transparent hover:bg-bg-surface text-text-muted hover:text-text'
		},
		size: {
			default: 'h-8 px-3 py-1.5',
			sm: 'h-7 px-2 text-xs',
			lg: 'h-9 px-4',
			icon: 'h-8 w-8'
		}
	},
	defaultVariants: {
		variant: 'default',
		size: 'default'
	}
});

export type ButtonVariant = VariantProps<typeof buttonVariants>['variant'];
export type ButtonSize = VariantProps<typeof buttonVariants>['size'];
