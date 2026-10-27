import type { ReactNode } from 'react'
import { ForbiddenPage } from '../pages/ErrorPages'
import { useAuth } from '../hooks/useAuth'

/**
 * Renders its children only when the caller holds the permission.
 *
 * This is a usability guard, not a security boundary. Every endpoint the page
 * calls checks the same permission on the server, so a user who reaches a page
 * another way still gets 403 rather than data.
 */
export function RequirePermission({
  permission,
  children,
}: {
  permission: string
  children: ReactNode
}) {
  const { can } = useAuth()
  if (!can(permission)) return <ForbiddenPage permission={permission} />
  return <>{children}</>
}
