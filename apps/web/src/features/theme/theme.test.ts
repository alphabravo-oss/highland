import { describe, expect, it } from 'vitest'
import { STORAGE_KEY } from './ThemeProvider'

describe('theme storage contract', () => {
  it('uses highland-theme localStorage key from plan §14.3', () => {
    expect(STORAGE_KEY).toBe('highland-theme')
  })
})
