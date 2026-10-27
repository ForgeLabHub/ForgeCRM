import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { emailApi } from '../../api/notifications'
import { errorMessage } from '../../api/client'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, Field, PageHeader, StatusBadge, Timestamp } from '../../components/ui'
import type { EmailLogEntry } from '../../api/notifications'

const logColumns: Column<EmailLogEntry>[] = [
  {
    key: 'to',
    header: 'Recipient',
    render: (row) => (
      <div>
        <div className="table__primary">{row.to_address}</div>
        {row.subject ? <div className="table__secondary truncate">{row.subject}</div> : null}
      </div>
    ),
  },
  { key: 'status', header: 'Status', render: (row) => <StatusBadge status={row.status} /> },
  {
    key: 'template',
    header: 'Template',
    render: (row) => (row.template ? <span className="mono text-sm">{row.template}</span> : <span className="text-subtle">—</span>),
  },
  { key: 'attempts', header: 'Attempts', numeric: true, render: (row) => row.attempts },
  {
    key: 'sent',
    header: 'Sent',
    render: (row) => <Timestamp value={row.sent_at} />,
  },
]

export function EmailPage() {
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
      <PageHeader
        title="Email"
        meta="Outbound mail configuration, delivery testing and the send log."
      />

      <div className="stack">
        <Card title="Configuration">
          {status.isError ? (
            <Alert tone="error">{errorMessage(status.error)}</Alert>
          ) : status.isLoading ? (
            <p className="text-muted">Loading…</p>
          ) : status.data ? (
            <div className="stack">
              <div className="row">
                {status.data.enabled ? (
                  <Badge tone="success">enabled</Badge>
                ) : (
                  <Badge tone="neutral">disabled</Badge>
                )}
                {status.data.configured ? (
                  <Badge tone="success">configured</Badge>
                ) : (
                  <Badge tone="warning">not configured</Badge>
                )}
                {status.data.tls ? <Badge tone="accent">TLS</Badge> : null}
              </div>
              <dl className="dl">
                <dt>Host</dt>
                <dd className="mono">{status.data.host ?? '—'}</dd>
                <dt>Port</dt>
                <dd className="mono">{status.data.port ?? '—'}</dd>
                <dt>From address</dt>
                <dd>{status.data.from ?? '—'}</dd>
                {status.data.last_error ? (
                  <>
                    <dt>Last error</dt>
                    <dd className="text-danger">{status.data.last_error}</dd>
                  </>
                ) : null}
              </dl>
            </div>
          ) : null}
        </Card>

        <Card title="Send a test message">
          <div className="stack">
            <p className="text-muted text-sm">
              This connects to the SMTP server and sends a real message to the address you give.
              A successful result means the server accepted it, not that it reached an inbox.
            </p>
            <div className="form-grid">
              <Field label="Recipient" htmlFor="email-test-to" required>
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
                {sendTest.isPending ? 'Sending…' : 'Send test'}
              </button>
            </div>

            {testResult ? (
              <Alert tone={testResult.ok ? 'success' : 'error'}>
                {testResult.ok
                  ? 'The SMTP server accepted the message.'
                  : `Failed: ${testResult.message ?? 'unknown error'}`}
              </Alert>
            ) : null}
          </div>
        </Card>

        <ResourceList
          title="Send log"
          queryKey={['email', 'log']}
          fetchPage={(params) => emailApi.log(params)}
          columns={logColumns}
          rowKey={(row) => row.id}
          emptyTitle="No messages sent"
          emptyText="Notifications and password resets appear here once they are queued."
          searchable
          searchPlaceholder="Search recipients…"
        />
      </div>
    </>
  )
}
