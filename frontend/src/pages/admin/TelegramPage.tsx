import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { telegramApi } from '../../api/notifications'
import { errorMessage } from '../../api/client'
import { Alert, Badge, Card, ConfirmDialog, PageHeader } from '../../components/ui'

export function TelegramPage() {
  const [linkCode, setLinkCode] = useState<{ code: string; expires_at: string } | null>(null)
  const [unlinking, setUnlinking] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const status = useQuery({ queryKey: ['telegram', 'status'], queryFn: () => telegramApi.status() })

  const requestCode = useMutation({
    mutationFn: () => telegramApi.linkCode(),
    onSuccess: (result) => {
      setLinkCode(result)
      setError(null)
    },
    onError: (err) => setError(errorMessage(err)),
  })

  const unlink = useMutation({
    mutationFn: () => telegramApi.unlink(),
    onSuccess: () => {
      setUnlinking(false)
      setLinkCode(null)
      setError(null)
    },
    onError: (err) => {
      setUnlinking(false)
      setError(errorMessage(err))
    },
  })

  return (
    <>
      <PageHeader
        title="Telegram"
        meta="Optional bot that can send you notifications and let you look at your own tasks."
      />

      <div className="stack">
        {error ? <Alert tone="error">{error}</Alert> : null}

        <Card title="Bot status">
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
                  <Badge tone="success">token set</Badge>
                ) : (
                  <Badge tone="warning">no token</Badge>
                )}
                {status.data.webhook_set ? <Badge tone="accent">webhook registered</Badge> : null}
              </div>

              {!status.data.configured ? (
                <Alert tone="info">
                  No bot token is configured. ForgeCRM runs normally without Telegram; set a token in
                  the server configuration to enable the bot.
                </Alert>
              ) : (
                <dl className="dl">
                  <dt>Bot</dt>
                  <dd className="mono">{status.data.bot_username ?? '—'}</dd>
                  {status.data.last_error ? (
                    <>
                      <dt>Last error</dt>
                      <dd className="text-danger">{status.data.last_error}</dd>
                    </>
                  ) : null}
                </dl>
              )}
            </div>
          ) : null}
        </Card>

        {status.data?.configured ? (
          <Card title="Link your account">
            <div className="stack">
              <p className="text-muted text-sm">
                Generate a code, then send it to the bot in Telegram. The code expires shortly and
                can only be used once. Linking is per user, so the bot always acts as you and is
                bound by your permissions.
              </p>

              <div className="row">
                <button
                  type="button"
                  className="btn btn--primary btn--sm"
                  onClick={() => requestCode.mutate()}
                  disabled={requestCode.isPending}
                >
                  {requestCode.isPending ? 'Generating…' : 'Generate link code'}
                </button>
                <button type="button" className="btn btn--danger btn--sm" onClick={() => setUnlinking(true)}>
                  Unlink my account
                </button>
              </div>

              {linkCode ? (
                <Alert tone="success">
                  Send this code to the bot: <strong className="mono">{linkCode.code}</strong>
                  <br />
                  It expires at {new Date(linkCode.expires_at).toLocaleTimeString()}.
                </Alert>
              ) : null}
            </div>
          </Card>
        ) : null}
      </div>

      <ConfirmDialog
        open={unlinking}
        title="Unlink your Telegram account?"
        text="The bot stops recognising you immediately and cannot read your tasks until you link again."
        confirmLabel="Unlink"
        danger
        busy={unlink.isPending}
        onConfirm={() => unlink.mutate()}
        onCancel={() => setUnlinking(false)}
      />
    </>
  )
}
