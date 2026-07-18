import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Popover, PopoverAnchor, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'

describe('card wrapper', () => {
  it('renders every card slot and forwards custom classes', () => {
    render(
      <Card className="card-extra">
        <CardHeader className="header-extra">
          <CardTitle className="title-extra">Requests</CardTitle>
          <CardDescription className="description-extra">Open customer asks</CardDescription>
          <CardAction className="action-extra">Refresh</CardAction>
        </CardHeader>
        <CardContent className="content-extra">Queue content</CardContent>
        <CardFooter className="footer-extra">Footer action</CardFooter>
      </Card>,
    )

    expect(screen.getByText('Requests')).toHaveAttribute('data-slot', 'card-title')
    expect(screen.getByText('Requests')).toHaveClass('title-extra')
    expect(screen.getByText('Open customer asks')).toHaveAttribute('data-slot', 'card-description')
    expect(screen.getByText('Refresh')).toHaveAttribute('data-slot', 'card-action')
    expect(screen.getByText('Queue content')).toHaveAttribute('data-slot', 'card-content')
    expect(screen.getByText('Footer action')).toHaveAttribute('data-slot', 'card-footer')
    expect(document.querySelector('[data-slot="card"]')).toHaveClass('card-extra')
    expect(document.querySelector('[data-slot="card-header"]')).toHaveClass('header-extra')
  })
})

describe('popover wrapper', () => {
  it('renders trigger, anchor, and portal content with default alignment props', () => {
    render(
      <Popover open>
        <PopoverAnchor data-testid="popover-anchor" />
        <PopoverTrigger>Open filters</PopoverTrigger>
        <PopoverContent className="popover-extra">Filter controls</PopoverContent>
      </Popover>,
    )

    expect(screen.getByText('Open filters')).toHaveAttribute('data-slot', 'popover-trigger')
    expect(screen.getByTestId('popover-anchor')).toHaveAttribute('data-slot', 'popover-anchor')
    expect(screen.getByText('Filter controls')).toHaveAttribute('data-slot', 'popover-content')
    expect(screen.getByText('Filter controls')).toHaveClass('popover-extra')
  })

  it('accepts explicit alignment and offset props for positioned content', () => {
    render(
      <Popover open>
        <PopoverTrigger>Open actions</PopoverTrigger>
        <PopoverContent align="start" side="right" sideOffset={12}>
          Action controls
        </PopoverContent>
      </Popover>,
    )

    expect(screen.getByText('Action controls')).toHaveAttribute('data-align', 'start')
    expect(screen.getByText('Action controls')).toHaveAttribute('data-side', 'right')
  })
})

describe('sheet wrapper', () => {
  it('renders the default close button and all semantic sheet slots', () => {
    render(
      <Sheet open>
        <SheetTrigger>Open drawer</SheetTrigger>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Request details</SheetTitle>
            <SheetDescription>Review the selected request</SheetDescription>
          </SheetHeader>
          <SheetFooter>
            <SheetClose>Dismiss</SheetClose>
          </SheetFooter>
        </SheetContent>
      </Sheet>,
    )

    expect(screen.getByText('Open drawer')).toHaveAttribute('data-slot', 'sheet-trigger')
    expect(screen.getByRole('dialog')).toHaveAttribute('data-slot', 'sheet-content')
    expect(screen.getByText('Request details')).toHaveAttribute('data-slot', 'sheet-title')
    expect(screen.getByText('Review the selected request')).toHaveAttribute(
      'data-slot',
      'sheet-description',
    )
    expect(screen.getByText('Dismiss')).toHaveAttribute('data-slot', 'sheet-close')
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
    expect(document.querySelector('[data-slot="sheet-overlay"]')).toBeInTheDocument()
  })

  it.each([
    'left',
    'top',
    'bottom',
  ] as const)('renders %s sheet content without the built-in close button', (side) => {
    render(
      <Sheet open>
        <SheetContent side={side} showCloseButton={false}>
          <SheetTitle>{side} drawer</SheetTitle>
        </SheetContent>
      </Sheet>,
    )

    expect(screen.getByText(`${side} drawer`)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Close' })).not.toBeInTheDocument()
  })
})

describe('select wrapper', () => {
  it('renders the closed trigger with size metadata and custom classes', () => {
    render(
      <Select value="daily">
        <SelectTrigger className="trigger-extra" size="sm">
          <SelectValue placeholder="Cadence" />
        </SelectTrigger>
      </Select>,
    )

    const trigger = screen.getByRole('combobox')
    expect(trigger).toHaveAttribute('data-slot', 'select-trigger')
    expect(trigger).toHaveAttribute('data-size', 'sm')
    expect(trigger).toHaveClass('trigger-extra')
  })

  it('renders grouped popper content with labels, items, and separators', () => {
    render(
      <Select open value="daily">
        <SelectTrigger>
          <SelectValue placeholder="Cadence" />
        </SelectTrigger>
        <SelectContent className="content-extra" position="popper" align="start">
          <SelectGroup>
            <SelectLabel className="label-extra">Cadence</SelectLabel>
            <SelectItem className="item-extra" value="daily">
              Daily
            </SelectItem>
            <SelectSeparator className="separator-extra" />
            <SelectItem value="weekly">Weekly</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>,
    )

    expect(screen.getByText('Cadence')).toHaveAttribute('data-slot', 'select-label')
    expect(screen.getByText('Cadence')).toHaveClass('label-extra')
    expect(screen.getByRole('option', { name: 'Daily' })).toHaveAttribute(
      'data-slot',
      'select-item',
    )
    expect(screen.getByRole('option', { name: 'Daily' })).toHaveClass('item-extra')
    expect(screen.getByRole('option', { name: 'Weekly' })).toHaveAttribute(
      'data-slot',
      'select-item',
    )
    expect(document.querySelector('[data-slot="select-content"]')).toHaveClass('content-extra')
    expect(document.querySelector('[data-slot="select-separator"]')).toHaveClass('separator-extra')
    expect(screen.getByRole('listbox')).toHaveAttribute('data-slot', 'select-content')
  })
})
