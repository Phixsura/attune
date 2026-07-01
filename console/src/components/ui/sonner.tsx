import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
} from 'lucide-react'
import { useTheme } from 'next-themes'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = 'system' } = useTheme()

  return (
    <Sonner
      theme={theme as ToasterProps['theme']}
      className="toaster group"
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
          '--success-bg': 'hsl(143 85% 96%)',
          '--success-border': 'hsl(145 55% 55%)',
          '--success-text': 'hsl(145 90% 18%)',
          '--info-bg': 'hsl(208 100% 97%)',
          '--info-border': 'hsl(214 75% 58%)',
          '--info-text': 'hsl(214 85% 28%)',
          '--warning-bg': 'hsl(49 100% 97%)',
          '--warning-border': 'hsl(35 80% 50%)',
          '--warning-text': 'hsl(31 92% 28%)',
          '--error-bg': 'hsl(0 86% 97%)',
          '--error-border': 'hsl(0 65% 55%)',
          '--error-text': 'hsl(0 82% 24%)',
          '--border-radius': 'var(--radius)',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
