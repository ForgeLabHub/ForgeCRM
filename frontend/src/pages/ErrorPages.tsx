import { Link } from 'react-router-dom'
import { EmptyState } from '../components/ui'
import { useT } from '../i18n'

export function ForbiddenPage({ permission }: { permission?: string }) {
  const t = useT()
  return (
    <div className="card">
      <EmptyState
        title={t('errors.forbiddenTitle')}
        text={permission ? t('errors.forbiddenPermission', { permission }) : t('errors.forbiddenText')}
        action={
          <Link to="/" className="btn btn--secondary">
            {t('errors.backToOverview')}
          </Link>
        }
      />
    </div>
  )
}

export function NotFoundPage() {
  const t = useT()
  return (
    <div className="card">
      <EmptyState
        title={t('errors.notFoundTitle')}
        text={t('errors.notFoundText')}
        action={
          <Link to="/" className="btn btn--secondary">
            {t('errors.backToOverview')}
          </Link>
        }
      />
    </div>
  )
}
