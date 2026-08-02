import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Merges class names, letting a later utility win over an earlier one.
 *
 * The shadcn convention. Plain concatenation looks like it works and then
 * fails on the case it exists for: `cn('p-2', 'p-4')` must be `p-4`, and with
 * concatenation the winner depends on the order the two rules happen to appear
 * in the generated stylesheet.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
