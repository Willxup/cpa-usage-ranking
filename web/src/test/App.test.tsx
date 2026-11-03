import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from '../App'

describe('application foundation', () => {
  it('shows the product and honestly identifies unfinished business', () => {
    render(<App />)
    expect(screen.getByRole('heading', { level: 1, name: 'CPA Usage Ranking' })).toBeVisible()
    expect(screen.getByText('基础框架正在搭建，排名业务尚未接入。')).toBeVisible()
  })
})
