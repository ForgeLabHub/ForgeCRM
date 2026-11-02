import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { auditApi, diagnosticsApi } from '../../api/admin'
import type { AuditChainVerification, LogLine } from '../../api/admin'
import { errorMessage } from '../../api/client'
import { ResourceList, type Column } from '../../components/ResourceList'
import { Alert, Badge, Card, PageHeader, Timestamp } from '../../components/ui'
import type { AuditEntry } from '../../types/api'
import { useT, type TFunction } from '../../i18n'

function buildAuditColumns(t: TFunction): Column<AuditEntry>[] {
  return [
  {
    key: 'action',
    header: t('auditLog.columnAction'),
    render: (row) => (
      <div>
        <div className="table__primary mono">{row.action}</div>
        {row.entity_type ? (
          <div className="table__secondary">
            {row.entity_type}
            {row.entity_id ? ` · ${row.entity_id.slice(0, 8)}` : ''}
          </div>
        ) : null}
      </div>
    ),
  },
  {
    key: 'actor',
    header: t('auditLog.columnActor'),
    render: (row) =>
      row.actor_user_id ? (
        <span className="mono text-sm">{row.actor_user_id.slice(0, 8)}</span>
      ) : (
        <Badge tone="neutral">{t('auditLog.system')}</Badge>
      ),
  },
  {
    key: 'ip',
    header: t('auditLog.columnSource'),
    render: (row) => row.ip ?? <span className="text-subtle">—</span>,
  },
  {
    key: 'correlation',
    header: t('auditLog.columnCorrelation'),
    render: (row) => (
      <span className="mono text-sm truncate" style={{ maxWidth: 180 }}>
        {row.correlation_id ?? '—'}
      </span>
    ),
    },
    {
    key: 'when',
    header: t('auditLog.columnWhen'),
    render: (row) => <Timestamp value={row.created_at} />,
    },
    ]
    }

export function AuditPage() {
  const t = useT()
  const [tab, setTab] = useState<'audit' | 'logs'>('audit')

  return (
    <>
      <PageHeader title={t('auditLog.title')} meta={t('auditLog.meta')} />

      <div className="row" style={{ marginBottom: 'var(--space-4)' }}>
        <button
          type="button"
          className={tab === 'audit' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('audit')}
          aria-pressed={tab === 'audit'}
        >
          {t('auditLog.tabAudit')}
          </button>
        <button
          type="button"
          className={tab === 'logs' ? 'btn btn--secondary btn--sm' : 'btn btn--ghost btn--sm'}
          onClick={() => setTab('logs')}
          aria-pressed={tab === 'logs'}
        >
          {t('auditLog.tabLogs')}
          </button>
      </div>

      {tab === 'audit' ? (
        <>
          <IntegrityCheck />
          <ResourceList
            title={t('auditLog.auditTitle')}
            queryKey={['audit']}
            fetchPage={(params) => auditApi.list(params)}
            columns={buildAuditColumns(t)}
            rowKey={(row) => row.id}
            emptyTitle={t('auditLog.emptyAuditTitle')}
            emptyText={t('auditLog.emptyAuditText')}
            searchable
            searchPlaceholder={t('auditLog.searchAudit')}
          />
        </>
      ) : (
        <ApplicationLog />
      )}
    </>
  )
}

/**
 * The tamper-evidence surface for the audit trail.
 *
 * The trail is a hash chain: each entry carries the hash of the one before it, so
 * removing or editing an entry breaks every link that follows. Verification is on
 * demand rather than on load, because it walks the whole chain and an operator
 * runs it when they want to make a claim about integrity, not on every visit.
 */
function IntegrityCheck() {
  const t = useT()
  const [result, setResult] = useState<AuditChainVerification | null>(null)
  const check = useMutation({
    mutationFn: () => auditApi.verify(),
    onSuccess: (data) => setResult(data),
  })

  return (
    <Card
      title={t('auditLog.integrityTitle')}
      actions={
        <button
          type="button"
          className="btn btn--secondary btn--sm"
          onClick={() => check.mutate()}
          disabled={check.isPending}
        >
          {check.isPending ? t('auditLog.verifying') : t('auditLog.verifyIntegrity')}
        </button>
      }
    >
      <p className="text-sm text-subtle" style={{ marginTop: 0 }}>
        {t('auditLog.integrityExplanation')}
      </p>

      {check.isError ? <Alert tone="error">{errorMessage(check.error)}</Alert> : null}

      {result ? (
        <Alert tone={result.intact ? 'success' : 'error'}>
          {result.intact
            ? `${t('auditLog.integrityIntact', {
                count: result.checked,
                entries: result.checked === 1 ? 'entry' : 'entries',
              })}${
                result.unchained > 0
                  ? t('auditLog.integrityUnchained', { count: result.unchained })
                  : ''
              }`
            : t('auditLog.integrityBroken', {
                id: String(result.first_break_id),
                reason: result.reason ?? '',
              })}
        </Alert>
      ) : null}
    </Card>
  )
}

function buildLogColumns(t: TFunction): Column<LogLine>[] {
  return [
    {
    key: 'level',
    header: t('auditLog.columnLevel'),
    render: (row) => (
      <Badge
        tone={
          row.level === 'error'
            ? 'danger'
            : row.level === 'warn'
              ? 'warning'
              : row.level === 'debug'
                ? 'neutral'
                : 'accent'
        }
      >
        {row.level}
      </Badge>
    ),
  },
  {
    key: 'message',
    header: t('auditLog.columnMessage'),
    render: (row) => (
      <div>
        <div className="table__primary">{row.message}</div>
        {row.component ? <div className="table__secondary">{row.component}</div> : null}
      </div>
    ),
  },
  {
    key: 'correlation',
    header: t('auditLog.columnCorrelation'),
    render: (row) => (
      <span className="mono text-sm truncate" style={{ maxWidth: 180 }}>
        {row.correlation_id ?? '—'}
      </span>
    ),
    },
    {
    key: 'time',
    header: t('auditLog.columnWhen'),
    render: (row) => <Timestamp value={row.time} />,
    },
    ]
    }

function ApplicationLog() {
  const t = useT()
  const [level, setLevel] = useState('')

  // A single cheap request decides whether the log is reachable at all, so a
  // permission problem shows as a clear message rather than an empty table.
  const logs = useQuery({
    queryKey: ['logs', 'probe', level],
    queryFn: () => diagnosticsApi.logs({ limit: 1, level: level || undefined }),
  })

  return (
    <>
      <Alert tone="info">{t('auditLog.redactionNote')}</Alert>

      <div style={{ marginTop: 'var(--space-4)' }}>
        {logs.isError ? (
          <Card title={t('auditLog.tabLogs')}>
            <Alert tone="error">{errorMessage(logs.error)}</Alert>
          </Card>
        ) : (
          <ResourceList
            title={t('auditLog.tabLogs')}
            queryKey={['logs', level]}
            fetchPage={(params) => diagnosticsApi.logs({ ...params, level: level || undefined })}
            columns={buildLogColumns(t)}
            rowKey={(row) => `${row.time}-${row.message}`}
            emptyTitle={t('auditLog.emptyLogTitle')}
            emptyText={t('auditLog.emptyLogText')}
            searchable
            searchPlaceholder={t('auditLog.searchLog')}
            filters={
              <label className="field" style={{ maxWidth: 160 }}>
                <span className="field__label">{t('auditLog.level')}</span>
                <select value={level} onChange={(event) => setLevel(event.target.value)}>
                  <option value="">{t('auditLog.allLevels')}</option>
                  {['debug', 'info', 'warn', 'error'].map((item) => (
                    <option key={item} value={item}>
                      {item}
                    </option>
                  ))}
                </select>
              </label>
            }
          />
        )}
      </div>
    </>
  )
}
