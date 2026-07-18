import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Progress } from '@/components/ui/progress'
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

describe('table wrapper', () => {
  it('renders every table slot and forwards custom classes', () => {
    render(
      <Table className="table-extra">
        <TableCaption className="caption-extra">Requests table</TableCaption>
        <TableHeader className="header-extra">
          <TableRow className="row-extra">
            <TableHead className="head-extra">Name</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className="body-extra">
          <TableRow>
            <TableCell className="cell-extra">Acme</TableCell>
          </TableRow>
        </TableBody>
        <TableFooter className="footer-extra">
          <TableRow>
            <TableCell>Total</TableCell>
          </TableRow>
        </TableFooter>
      </Table>,
    )

    expect(document.querySelector('[data-slot="table-container"]')).toBeInTheDocument()
    expect(document.querySelector('[data-slot="table"]')).toHaveClass('table-extra')
    expect(document.querySelector('[data-slot="table-header"]')).toHaveClass('header-extra')
    expect(document.querySelector('[data-slot="table-body"]')).toHaveClass('body-extra')
    expect(document.querySelector('[data-slot="table-footer"]')).toHaveClass('footer-extra')
    expect(screen.getByText('Requests table')).toHaveAttribute('data-slot', 'table-caption')
    expect(screen.getByText('Requests table')).toHaveClass('caption-extra')
    expect(screen.getByText('Name')).toHaveAttribute('data-slot', 'table-head')
    expect(screen.getByText('Name')).toHaveClass('head-extra')
    expect(screen.getByText('Acme')).toHaveAttribute('data-slot', 'table-cell')
    expect(screen.getByText('Acme')).toHaveClass('cell-extra')
    expect(document.querySelector('[data-slot="table-row"]')).toHaveClass('row-extra')
  })
})

describe('progress wrapper', () => {
  it('uses the provided value to position the indicator', () => {
    render(<Progress className="progress-extra" value={35} />)

    expect(document.querySelector('[data-slot="progress"]')).toHaveClass('progress-extra')
    expect(document.querySelector('[data-slot="progress-indicator"]')).toHaveStyle({
      transform: 'translateX(-65%)',
    })
  })

  it('treats an omitted value as zero progress', () => {
    render(<Progress />)

    expect(document.querySelector('[data-slot="progress-indicator"]')).toHaveStyle({
      transform: 'translateX(-100%)',
    })
  })
})
