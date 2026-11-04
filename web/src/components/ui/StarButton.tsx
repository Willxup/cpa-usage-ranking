// Adapted from React Bits StarBorder (CSS variant), revision c83b5e7.
// Copyright (c) 2025 David Haz. MIT + Commons Clause; see THIRD_PARTY_NOTICES.md.
// Native button semantics, app tokens and disabled/focus behavior are local adaptations.
import type { ComponentProps } from 'react'
import './StarButton.css'

export function StarButton({ children, className = '', type = 'button', ...props }: ComponentProps<'button'>) {
  return (
    <button {...props} type={type} className={`star-button ${className}`}>
      <span className="star-button__trail star-button__trail--bottom" aria-hidden="true" />
      <span className="star-button__trail star-button__trail--top" aria-hidden="true" />
      <span className="star-button__content">{children}</span>
    </button>
  )
}
