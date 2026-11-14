import type { AnchorHTMLAttributes, MouseEvent } from 'react'
import { isAppRoute, navigate } from './navigation'

export function Link({ href = '/', onClick, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) {
  function click(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event)
    if (!event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !props.target && !props.download && isAppRoute(href)) {
      event.preventDefault(); navigate(href)
    }
  }
  return <a {...props} href={href} onClick={click} />
}
