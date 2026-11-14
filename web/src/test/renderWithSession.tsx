import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { SessionProvider } from '../admin/SessionProvider'

export function renderWithSession(ui: ReactNode) { return render(<SessionProvider>{ui}</SessionProvider>) }
