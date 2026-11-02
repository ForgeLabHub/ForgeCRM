import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { emailApi } from '../../api/notifications'
import { errorMessage } from '../../api/client'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, Field, PageHeader, StatusBadge, Timestamp } from '../../components/ui'
import { useT, type TFunction } from '../../i18n'
import type { EmailLogEntry } from '../../api/notifications'

function buildLogColumns(t: TFunction): Column<EmailLogEntry>[] {
  return [
  {
    key: 'to',
    header: t('emailAdmin.columnRecipient'),
    render: (row) => (
      <div>
        <div className="table__primary">{row.to_address}</div>
        {row.subject ? <div className="table__secondary truncate">{row.subject}</div> : null}
      </div>
    ),
  },
  { key: 'status', header: t('emailAdmin.columnStatus'), render: (row) => <StatusBadge status={row.status} /> },
  {
    key: 'template',
    header: t('emailAdmin.columnTemplate'),
    render: (row) => (row.template ? <span className="mono text-sm">{row.template}</span> : <span className="text-subtle">—</span>),
  },
  { key: 'attempts', header: t('emailAdmin.columnAttempts'), numeric: true, render: (row) => row.attempts },
  {
    key: 'sent',
    header: t('emailAdmin.columnSent'),
    render: (row) => <Timestamp value={row.sent_at} />,
  },
  ]
}

export function EmailPage() {
  const t = useT()
  const queryClient = useQueryClient()
  const [testAddress, setTestAddress] = useState('')
  const [testResult, setTestResult] = useState<{ ok: boolean; message?: string } | null>(null)

  const status = useQuery({ queryKey: ['email', 'status'], queryFn: () => emailApi.status() })

  const sendTest = useMutation({
    mutationFn: () => emailApi.test(testAddress),
    onSuccess: (result) => {
      setTestResult(result)
      void queryClient.invalidateQueries({ queryKey: ['email', 'log'] })
    },
    onError: (error) => setTestResult({ ok: false, message: errorMessage(error) }),
  })

  return (
    <>
      <PageHeader title={t('emailAdmin.title')} meta={t('emailAdmin.meta')} />

      <div className="stack">
        <Card title={t('emailAdmin.configuration')}>
          {status.isError ? (
            <Alert tone="error">{errorMessage(status.error)}</Alert>
          ) : status.isLoading ? (
            <p className="text-muted">{t('common.loading')}</p>
          ) : status.data ? (
            <div className="stack">
              <div className="row">
                {status.data.enabled ? (
                  <Badge tone="success">{t('emailAdmin.enabled')}</Badge>
                ) : (
                  <Badge tone="neutral">{t('emailAdmin.disabled')}</Badge>
                )}
                {status.data.configured ? (
                  <Badge tone="success">{t('emailAdmin.configured')}</Badge>
                ) : (
                  <Badge tone="warning">{t('emailAdmin.notConfigured')}</Badge>
                )}
                {status.data.tls ? <Badge tone="accent">TLS</Badge> : null}
              </div>
              <dl className="dl">
                <dt>{t('emailAdmin.host')}</dt>
                <dd className="mono">{status.data.host ?? '—'}</dd>
                <dt>{t('emailAdmin.port')}</dt>
                <dd className="mono">{status.data.port ?? '—'}</dd>
                <dt>{t('emailAdmin.fromAddress')}</dt>
                <dd>{status.data.from ?? '—'}</dd>
                {status.data.last_error ? (
                  <>
                    <dt>{t('emailAdmin.lastError')}</dt>
                    <dd className="text-danger">{status.data.last_error}</dd>
                  </>
                ) : null}
              </dl>
            </div>
          ) : null}
        </Card>

        <Card title={t('emailAdmin.sendTestTitle')}>
          <div className="stack">
            <p className="text-muted text-sm">{t('emailAdmin.sendTestIntro')}</p>
            <div className="form-grid">
              <Field label={t('emailAdmin.recipient')} htmlFor="email-test-to" required>
                <input
                  id="email-test-to"
                  type="email"
                  value={testAddress}
                  onChange={(event) => setTestAddress(event.target.value)}
                  placeholder="you@example.com"
                />
              </Field>
            </div>
            <div className="row">
              <button
                type="button"
                className="btn btn--primary btn--sm"
                disabled={!testAddress || sendTest.isPending}
                onClick={() => sendTest.mutate()}
              >
                {sendTest.isPending ? t('emailAdmin.sending') : t('emailAdmin.sendTest')}
              </button>
            </div>

            {testResult ? (
              <Alert tone={testResult.ok ? 'success' : 'error'}>
                {testResult.ok
                  ? t('emailAdmin.accepted')
                  : t('emailAdmin.failed', { message: testResult.message ?? t('emailAdmin.unknownError') })}
              </Alert>
            ) : null}
          </div>
        </Card>

        <ResourceList
          title={t('emailAdmin.logTitle')}
          queryKey={['email', 'log']}
          fetchPage={(params) => emailApi.log(params)}
          columns={buildLogColumns(t)}
          rowKey={(row) => row.id}
          emptyTitle={t('emailAdmin.emptyTitle')}
          emptyText={t('emailAdmin.emptyText')}
          searchable
          searchPlaceholder={t('emailAdmin.searchPlaceholder')}
        />
      </div>
    </>
  )
}
