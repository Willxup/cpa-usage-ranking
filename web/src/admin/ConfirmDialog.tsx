import { useEffect, useRef, useState } from 'react'
import { formatLocalizedError, useI18n } from '../i18n'

export function ConfirmDialog({ title, detail, onCancel, onConfirm }: { title: string; detail?: string; onCancel: () => void; onConfirm: () => Promise<void> }) {
  const { locale, m } = useI18n()
  const dialog = useRef<HTMLDialogElement>(null)
  const running = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const errorMessage = error == null ? '' : formatLocalizedError(error, (msg) => msg.admin.errors.network, locale)
  useEffect(() => {
    const element = dialog.current!
    const trigger = document.activeElement as HTMLElement | null
    element.showModal()
    element.querySelector<HTMLButtonElement>('button')?.focus()
    return () => { element.close(); if (trigger?.isConnected) trigger.focus() }
  }, [])
  async function confirm() {
    if (running.current) return
    running.current = true; setBusy(true); setError(null)
    try { await onConfirm(); onCancel() }
    catch (err) { setError(err ?? true) }
    finally { running.current = false; setBusy(false) }
  }
  return <dialog ref={dialog} className="confirm-dialog" aria-labelledby="confirm-title" onCancel={(event) => { event.preventDefault(); if (!busy) onCancel() }}>
    <h2 id="confirm-title">{title}</h2>
    {detail && <p>{detail}</p>}
    {errorMessage && <p className="admin-error" role="alert">{errorMessage}</p>}
    <div className="confirm-actions"><button className="admin-button" type="button" disabled={busy} onClick={onCancel}>{m.admin.dialogCancel}</button><button className="admin-button admin-button--primary" type="button" disabled={busy} onClick={() => void confirm()}>{m.admin.dialogConfirm}</button></div>
  </dialog>
}
