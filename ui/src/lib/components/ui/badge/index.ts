import { tv, type VariantProps } from "tailwind-variants";

export const badgeVariants = tv({
  base: "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium transition-colors",
  variants: {
    variant: {
      default: "border-primary/50 bg-primary/20 text-primary",
      secondary: "border-border bg-muted text-muted-foreground",
      destructive: "border-destructive/20 bg-destructive/10 text-destructive",
      outline: "border-border text-muted-foreground",
      amber: "border-amber-muted bg-amber-muted text-amber",
    },
  },
  defaultVariants: {
    variant: "default",
  },
});

export type BadgeVariant = VariantProps<typeof badgeVariants>["variant"];
