import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

// Standard shadcn helper. Use everywhere instead of bare string template
// for className composition — `tailwind-merge` collapses conflicting
// utilities (e.g. `p-2 p-4` → `p-4`) which is essential for variant
// overrides.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
