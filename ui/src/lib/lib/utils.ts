import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export {
  type WithoutChildren,
  type WithoutChild,
  type WithElementRef,
  type WithoutChildrenOrChild,
} from "bits-ui";
