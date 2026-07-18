import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { HighlandVersionBadge } from './HighlandVersionBadge'

vi.mock('@/api/hooks', () => ({
  useCompatibility: () => ({
    data: {
      highlandVersion: '0.4.0',
      longhornSupport: ['1.12.x'],
    },
  }),
}))

vi.mock('@/i18n/useAppTranslation', () => ({
  useAppTranslation: () => ({
    t: (key: string) => (key === 'app.name' ? 'Highland' : key),
  }),
}))

describe('HighlandVersionBadge', () => {
  it('shows provider-neutral Highland branding when expanded', () => {
    render(<HighlandVersionBadge />)

    const badge = screen.getByTestId('highland-version-badge')
    expect(badge).toHaveTextContent('Highland v0.4.0')
    expect(badge).not.toHaveTextContent('LH')
  })

  it('shows a compact version when the sidebar is collapsed', () => {
    render(<HighlandVersionBadge collapsed />)

    const badge = screen.getByTestId('highland-version-badge')
    expect(badge).toHaveTextContent('v0.4')
    expect(badge).toHaveAttribute('title', 'Highland v0.4.0')
  })
})
