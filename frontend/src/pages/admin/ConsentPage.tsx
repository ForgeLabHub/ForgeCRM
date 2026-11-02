/**
 * Consent lookup by email address.
 *
 * The panel on a contact covers someone already in the CRM. This page covers the
 * other case: an address we are about to write to that is not a contact yet. Without
 * it the only reachable decision for such an address is "never asked", which the
 * policy blocks — so a message that should be allowed is refused with no way to
 * record the answer that would allow it.
 *
 * The lookup is the same record and the same panel, addressed by email instead of by
 * contact id, so the two surfaces cannot disagree about what a decision means.
 */
import { useState } from 'react'
import type { FormEvent } from 'react'
import { ConsentPanel } from '../../components/ConsentPanel'
import { Card, Field, PageHeader } from '../../components/ui'
import { useT } from '../../i18n'

export function ConsentPage() {
  const t = useT()
  const [input, setInput] = useState('')
  // The address the panel is actually about, committed on submit. The panel keys its
  // queries off this, so an address is never queried while it is still being typed.
  const [subject, setSubject] = useState('')

  const onSubmit = (event: FormEvent) => {
    event.preventDefault()
    setSubject(input.trim())
  }

  return (
    <>
      <PageHeader title={t('consentLookup.title')} meta={t('consentLookup.meta')} />

      <div className="stack">
        <Card title={t('consentLookup.lookupTitle')}>
          <form className="form-grid" onSubmit={onSubmit}>
            <Field
              label={t('consentLookup.emailLabel')}
              htmlFor="consent-lookup-email"
              hint={t('consentLookup.emailHint')}
            >
              <input
                id="consent-lookup-email"
                type="email"
                className="input"
                value={input}
                onChange={(event) => setInput(event.target.value)}
                placeholder="person@example.com"
              />
            </Field>
            <div className="form-grid__full">
              <button type="submit" className="btn btn--primary btn--sm" disabled={!input.trim()}>
                {t('consentLookup.lookup')}
              </button>
            </div>
          </form>
        </Card>

        {subject ? (
          <ConsentPanel subject={{ email: subject }} subjectLabel={subject} />
        ) : null}
      </div>
    </>
  )
}
