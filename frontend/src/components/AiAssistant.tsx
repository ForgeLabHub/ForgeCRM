import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { api, errorMessage } from '../api/client'
import { readCrm } from '../api/assistantCrm'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import './AiAssistant.css'

/** The existing chat endpoint is a single completion, not an agent runtime. */
export function AiAssistantChat() {
  const t = useT()
  const { can } = useAuth()
  const questionId = useId()
  const [prompt, setPrompt] = useState('')
  const [answer, setAnswer] = useState('')
  const [crmMode, setCrmMode] = useState(false)
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])

  async function submit(event: FormEvent) {
    event.preventDefault()
    const question = prompt.trim()
    if (!question || pending || !can('ai.tools.execute')) return
    const request = new AbortController()
    controller.current = request
    setPending(true)
    setAnswer('')
    setError('')
    let done = false
    try {
      if (crmMode) {
        if (!can('ai.read')) throw new Error(t('assistant.noPermission'))
        const result = await readCrm(question, can, request.signal)
        if (!request.signal.aborted) setAnswer(`${result.tool}\n${JSON.stringify(result.result, null, 2)}`)
        return
      }
      await api.stream('/api/v1/ai/stream/chat', {
        prompt: question,
        system: 'You are a helpful ForgeCRM assistant. You have no access to CRM records or tools in this chat. Never claim that you read or changed CRM data, or that an action was executed. Respond in the language of the user.',
      }, (event) => {
        if (event.event === 'delta' && typeof event.data === 'object' && event.data !== null && 'text' in event.data && typeof event.data.text === 'string') {
          setAnswer((previous) => previous + (event.data as { text: string }).text)
        }
        if (event.event === 'done') done = true
      }, request.signal)
      if (!done && !request.signal.aborted) setError(t('assistant.incomplete'))
    } catch (err) {
      if (!request.signal.aborted) setError(errorMessage(err))
    } finally {
      if (controller.current === request) {
        controller.current = null
        setPending(false)
      }
    }
  }

  return (
    <div className="assistant-chat">
      <p className="assistant-chat__hint">{t('assistant.limits')}</p>
      {!can('ai.tools.execute') ? <p role="status">{t('assistant.noPermission')}</p> : (
        <form onSubmit={submit} className="assistant-chat__form">
          {can('ai.read') ? <label><input type="checkbox" checked={crmMode} onChange={(event) => setCrmMode(event.target.checked)} disabled={pending} /> {t('assistant.crmMode')}</label> : null}
          {crmMode && can('ai.read') ? <p className="assistant-chat__hint">{t('assistant.crmHint')}</p> : null}
          <label htmlFor={questionId}>{t('assistant.question')}</label>
          <textarea id={questionId} value={prompt} onChange={(event) => setPrompt(event.target.value)} maxLength={4000} rows={3} placeholder={t('assistant.placeholder')} disabled={pending} />
          <div className="assistant-chat__actions">
            <button className="btn btn--primary" type="submit" disabled={pending || !prompt.trim()}>{pending ? t('assistant.working') : t('assistant.send')}</button>
            {pending ? <button className="btn btn--secondary" type="button" onClick={() => controller.current?.abort()}>{t('assistant.stop')}</button> : null}
          </div>
        </form>
      )}
      {error ? <p role="alert" className="assistant-chat__error">{error}</p> : null}
      {answer ? <div className="assistant-chat__answer" aria-live="polite"><strong>{t('assistant.answer')}</strong><p>{answer}</p></div> : null}
      <div className="assistant-chat__links">
        {can('ai.read') ? <Link to="/app/ai">{t('assistant.openPage')}</Link> : null}
        {can('approvals.read') ? <Link to="/app/approvals">{t('assistant.approvals')}</Link> : null}
      </div>
    </div>
  )
}

export function AiAssistantLauncher() {
  const t = useT()
  const { status, can, session } = useAuth()
  const location = useLocation()
  const [open, setOpen] = useState(false)
  // Do not expose a session's conversation after sign-out.
  useEffect(() => { if (status !== 'authenticated') setOpen(false) }, [status])
  if (status === 'loading' || location.pathname === '/login') return null
  if (status === 'authenticated' && !can('ai.tools.execute') && !can('ai.read')) return null
  return (
    <aside className="assistant-launcher" aria-label={t('assistant.title')}>
      {open ? (
        <section className="assistant-launcher__panel" aria-label={t('assistant.title')}>
          <header className="assistant-launcher__header"><strong>{t('assistant.title')}</strong><button type="button" onClick={() => setOpen(false)} aria-label={t('assistant.close')}>×</button></header>
          {status === 'authenticated' ? <AiAssistantChat key={`${session?.organization.id}:${session?.user.id}`} /> : <p className="assistant-launcher__login">{t('assistant.loginHint')} <Link to="/login">{t('assistant.login')}</Link></p>}
        </section>
      ) : null}
      <button type="button" className="assistant-launcher__button" onClick={() => setOpen(!open)} aria-expanded={open}>
        ✦ {t('assistant.title')}
      </button>
    </aside>
  )
}
