import { tv, type VariantProps } from "tailwind-variants";

export const badgeVariants = tv({
  base: "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium transition-colors",
  variants: {
    variant: {
      default: "border-accent-strong bg-accent-muted text-accent",
      secondary: "border-border bg-bg-surface text-text-muted",
      destructive: "border-red-muted bg-red-muted text-red",
      outline: "border-border text-text-muted",
      amber: "border-amber-muted bg-amber-muted text-amber",
    },
  },
  defaultVariants: {
    variant: "default",
  },
});

export type BadgeVariant = VariantProps<typeof badgeVariants>["variant"];
