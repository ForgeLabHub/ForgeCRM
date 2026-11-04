import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { telegramApi } from '../../api/notifications'
import { errorMessage } from '../../api/client'
import { Alert, Badge, Card, ConfirmDialog, PageHeader } from '../../components/ui'
import { useT } from '../../i18n'

export function TelegramPage() {
  const t = useT()
  const [linkCode, setLinkCode] = useState<{ code: string; expires_in_seconds: number } | null>(null)
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
      <PageHeader title={t('telegram.title')} meta={t('telegram.meta')} />

      <div className="stack">
        {error ? <Alert tone="error">{error}</Alert> : null}

        <Card title={t('telegram.botStatus')}>
          {status.isError ? (
            <Alert tone="error">{errorMessage(status.error)}</Alert>
          ) : status.isLoading ? (
            <p className="text-muted">{t('common.loading')}</p>
          ) : status.data ? (
            <div className="stack">
              <div className="row">
                {status.data.enabled ? (
                  <Badge tone="success">{t('telegram.enabled')}</Badge>
                ) : (
                  <Badge tone="neutral">{t('telegram.disabled')}</Badge>
                )}
                {status.data.enabled ? (
                  <Badge tone="success">{t('telegram.tokenSet')}</Badge>
                ) : null}
              </div>

              {!status.data.enabled ? (
                <Alert tone="info">{t('telegram.noTokenInfo')}</Alert>
              ) : status.data.linking_available ? (
                <Alert tone="info">{t('telegram.linkingConfiguredNotice')}</Alert>
              ) : null}
            </div>
          ) : null}
        </Card>

        {status.data?.enabled && !status.data.linking_available ? (
          <Alert tone="info">{t('telegram.linkingUnavailable')}</Alert>
        ) : null}

        {status.data?.enabled && status.data.linking_available ? (
          <Card title={t('telegram.linkAccount')}>
            <div className="stack">
              <p className="text-muted text-sm">{t('telegram.linkIntro')}</p>

              <div className="row">
                <button
                  type="button"
                  className="btn btn--primary btn--sm"
                  onClick={() => requestCode.mutate()}
                  disabled={requestCode.isPending}
                >
                  {requestCode.isPending ? t('telegram.generating') : t('telegram.generateCode')}
                </button>
              </div>

              {linkCode ? (
                <Alert tone="success">
                  {t('telegram.codeMessage')}
                  <strong className="mono">{linkCode.code}</strong>
                  <br />
                  {t('telegram.codeExpires', {
                    seconds: linkCode.expires_in_seconds,
                  })}
                </Alert>
              ) : null}
            </div>
          </Card>
        ) : null}
        {status.data ? (
          <button type="button" className="btn btn--danger btn--sm" onClick={() => setUnlinking(true)}>
            {t('telegram.unlinkAccount')}
          </button>
        ) : null}
      </div>

      <ConfirmDialog
        open={unlinking}
        title={t('telegram.unlinkTitle')}
        text={t('telegram.unlinkText')}
        confirmLabel={t('telegram.unlink')}
        danger
        busy={unlink.isPending}
        onConfirm={() => unlink.mutate()}
        onCancel={() => setUnlinking(false)}
      />
    </>
  )
}
