import { useEffect, useRef, useState } from 'react'

export function ConfirmDialog({ title, detail, onCancel, onConfirm }: { title: string; detail?: string; onCancel: () => void; onConfirm: () => Promise<void> }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const running = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    const element = dialog.current!
    const trigger = document.activeElement as HTMLElement | null
    element.showModal()
    element.querySelector<HTMLButtonElement>('button')?.focus()
    return () => { element.close(); if (trigger?.isConnected) trigger.focus() }
  }, [])
  async function confirm() {
    if (running.current) return
    running.current = true; setBusy(true); setError('')
    try { await onConfirm(); onCancel() }
    catch (error) { setError((error as Error).message) }
    finally { running.current = false; setBusy(false) }
  }
  return <dialog ref={dialog} className="confirm-dialog" aria-labelledby="confirm-title" onCancel={(event) => { event.preventDefault(); if (!busy) onCancel() }}>
    <h2 id="confirm-title">{title}</h2>
    {detail && <p>{detail}</p>}
    {error && <p className="admin-error" role="alert">{error}</p>}
    <div className="confirm-actions"><button className="admin-button" type="button" disabled={busy} onClick={onCancel}>取消</button><button className="admin-button admin-button--primary" type="button" disabled={busy} onClick={() => void confirm()}>确认</button></div>
  </dialog>
}
