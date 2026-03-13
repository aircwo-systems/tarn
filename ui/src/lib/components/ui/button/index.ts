import { tv, type VariantProps } from "tailwind-variants";

export const buttonVariants = tv({
  base: "inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
  variants: {
    variant: {
      default: "bg-primary text-primary-foreground hover:bg-primary/90",
      secondary: "border border-border bg-muted text-foreground hover:bg-popover",
      ghost: "hover:bg-muted text-muted-foreground hover:text-foreground",
      outline:
        "border border-border bg-transparent hover:bg-muted text-muted-foreground hover:text-foreground",
    },
    size: {
      default: "h-8 px-3 py-1.5",
      sm: "h-7 px-2 text-xs",
      lg: "h-9 px-4",
      icon: "h-8 w-8",
    },
  },
  defaultVariants: {
    variant: "default",
    size: "default",
  },
});

export type ButtonVariant = VariantProps<typeof buttonVariants>["variant"];
export type ButtonSize = VariantProps<typeof buttonVariants>["size"];
