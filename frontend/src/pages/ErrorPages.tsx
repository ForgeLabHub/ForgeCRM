import { Link } from 'react-router-dom'
import { EmptyState } from '../components/ui'

export function ForbiddenPage({ permission }: { permission?: string }) {
  return (
    <div className="card">
      <EmptyState
        title="You do not have access to this area"
        text={
          permission
            ? `This page requires the "${permission}" permission. Ask an administrator if you need it.`
            : 'Ask an administrator if you need access.'
        }
        action={
          <Link to="/" className="btn btn--secondary">
            Back to overview
          </Link>
        }
      />
    </div>
  )
}

export function NotFoundPage() {
  return (
    <div className="card">
      <EmptyState
        title="Page not found"
        text="The page you asked for does not exist."
        action={
          <Link to="/" className="btn btn--secondary">
            Back to overview
          </Link>
        }
      />
    </div>
  )
}
