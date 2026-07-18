import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Toaster } from '@/components/ui/sonner'

vi.mock('next-themes', () => ({
  useTheme: () => ({ theme: 'dark' }),
}))

vi.mock('sonner', () => ({
  Toaster: (props: {
    theme?: string
    className?: string
    icons?: Record<string, unknown>
    visibleToasts?: number
  }) => (
    <div
      className={props.className}
      data-icons={Object.keys(props.icons ?? {})
        .sort()
        .join(',')}
      data-testid="sonner-toaster"
      data-theme={props.theme}
      data-visible-toasts={props.visibleToasts}
    />
  ),
}))

describe('ui wrappers', () => {
  it('renders dropdown menu wrappers with expected slots and state helpers', () => {
    render(
      <DropdownMenu open>
        <DropdownMenuTrigger>Open actions</DropdownMenuTrigger>
        <DropdownMenuPortal>
          <span data-testid="portal-child">portal child</span>
        </DropdownMenuPortal>
        <DropdownMenuContent forceMount className="custom-content">
          <DropdownMenuLabel inset>Actions</DropdownMenuLabel>
          <DropdownMenuGroup>
            <DropdownMenuItem inset variant="destructive">
              Delete
              <DropdownMenuShortcut>Del</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuCheckboxItem checked>Subscribed</DropdownMenuCheckboxItem>
            <DropdownMenuRadioGroup value="daily">
              <DropdownMenuRadioItem value="daily">Daily</DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
            <DropdownMenuSeparator />
            <DropdownMenuSub open>
              <DropdownMenuSubTrigger inset>More</DropdownMenuSubTrigger>
              <DropdownMenuSubContent forceMount>
                <DropdownMenuItem>Archive</DropdownMenuItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>,
    )

    expect(document.querySelector('[data-slot="dropdown-menu-trigger"]')).toHaveAttribute(
      'data-slot',
      'dropdown-menu-trigger',
    )
    expect(screen.getByTestId('portal-child')).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'DeleteDel' })).toHaveAttribute(
      'data-variant',
      'destructive',
    )
    expect(screen.getByText('Actions')).toHaveAttribute('data-inset', 'true')
    expect(screen.getByText('Subscribed')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('Daily')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('Del')).toHaveAttribute('data-slot', 'dropdown-menu-shortcut')
    expect(screen.getByText('More')).toHaveAttribute('data-inset', 'true')
    expect(screen.getByText('Archive')).toBeInTheDocument()
  })

  it('passes the active theme and notification icons to sonner', () => {
    render(<Toaster visibleToasts={1} />)

    const toaster = screen.getByTestId('sonner-toaster')
    expect(toaster).toHaveAttribute('data-theme', 'dark')
    expect(toaster).toHaveAttribute('data-visible-toasts', '1')
    expect(toaster).toHaveAttribute('data-icons', 'error,info,loading,success,warning')
  })
})
