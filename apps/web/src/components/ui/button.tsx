import { cva, type VariantProps } from 'class-variance-authority'
import type { ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium transition-all active:scale-[0.98] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--color-background)] disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default:
          'bg-[var(--color-primary)] text-[var(--color-primary-foreground)] shadow-[var(--shadow-sm)] hover:opacity-92',
        secondary:
          'bg-[var(--color-secondary)] text-[var(--color-secondary-foreground)] hover:opacity-90',
        outline:
          'border border-[var(--color-border)] bg-[var(--color-card)] shadow-[var(--shadow-sm)] hover:bg-[var(--color-accent)]',
        ghost: 'hover:bg-[var(--color-accent)]',
        destructive:
          'bg-[var(--color-destructive)] text-[var(--color-destructive-foreground)] shadow-[var(--shadow-sm)] hover:opacity-92',
        link: 'text-[var(--color-primary)] underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs font-semibold',
        lg: 'h-10 rounded-md px-5',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants>

export function Button({ className, variant, size, ...props }: ButtonProps) {
  return (
    <button className={cn(buttonVariants({ variant, size }), className)} {...props} />
  )
}
