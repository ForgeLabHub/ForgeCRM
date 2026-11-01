/**
 * Sidebar icons.
 *
 * One small, consistent set drawn on a 24px grid with 1.6px strokes, so the
 * navigation reads at a glance without a font dependency or an image sprite.
 * Each icon is decorative: the link's text is its accessible name, so every
 * glyph is aria-hidden.
 */
import type { ReactNode } from 'react'

function Icon({ children }: { children: ReactNode }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {children}
    </svg>
  )
}

/** Maps a navigation path to its icon. A path without an entry renders none. */
export const NAV_ICONS: Record<string, ReactNode> = {
  '/': (
    <Icon>
      <path d="M4 13h6V4H4zM14 20h6v-9h-6zM4 20h6v-4H4zM14 8h6V4h-6z" />
    </Icon>
  ),
  '/accounts': (
    <Icon>
      <path d="M3 21h18M5 21V7l7-4 7 4v14M9 21v-5h6v5M9 11h.01M15 11h.01" />
    </Icon>
  ),
  '/contacts': (
    <Icon>
      <circle cx="12" cy="8" r="3.2" />
      <path d="M5 20a7 7 0 0114 0" />
    </Icon>
  ),
  '/leads': (
    <Icon>
      <path d="M3 17l5-5 4 4 8-8" />
      <path d="M15 8h5v5" />
    </Icon>
  ),
  '/deals': (
    <Icon>
      <path d="M3 7h18v12H3zM3 11h18M8 7V5h8v2" />
    </Icon>
  ),
  '/tasks': (
    <Icon>
      <path d="M9 6h11M9 12h11M9 18h11" />
      <path d="M4 6l1 1 2-2M4 12l1 1 2-2M4 18l1 1 2-2" />
    </Icon>
  ),
  '/tickets': (
    <Icon>
      <path d="M4 8a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 000 4v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2a2 2 0 000-4z" />
      <path d="M13 6v12" strokeDasharray="2 2" />
    </Icon>
  ),
  '/reports': (
    <Icon>
      <path d="M4 20V4M4 20h16" />
      <path d="M8 16v-5M12 16V8M16 16v-3" />
    </Icon>
  ),
  '/simulation': (
    <Icon>
      <path d="M12 3v4M12 17v4M3 12h4M17 12h4" />
      <circle cx="12" cy="12" r="3.2" />
    </Icon>
  ),
  '/forecast': (
    <Icon>
      <path d="M4 19l5-6 4 3 7-9" />
      <path d="M20 7h-4M20 7v4" />
    </Icon>
  ),
  '/risk': (
    <Icon>
      <path d="M12 3l9 16H3z" />
      <path d="M12 10v4M12 17h.01" />
    </Icon>
  ),
  '/imports': (
    <Icon>
      <path d="M12 3v10M8 9l4 4 4-4" />
      <path d="M4 17v2a2 2 0 002 2h12a2 2 0 002-2v-2" />
    </Icon>
  ),
  '/migrations': (
    <Icon>
      <path d="M4 8h13l-3-3M20 16H7l3 3" />
    </Icon>
  ),
  '/integrations': (
    <Icon>
      <path d="M9 3v4M15 3v4M7 7h10v4a5 5 0 01-10 0zM12 16v5" />
    </Icon>
  ),
  '/webhooks': (
    <Icon>
      <circle cx="7" cy="12" r="2.5" />
      <circle cx="17" cy="6" r="2.5" />
      <circle cx="17" cy="18" r="2.5" />
      <path d="M9.2 10.8l5.6-3.6M9.2 13.2l5.6 3.6" />
    </Icon>
  ),
  '/events': (
    <Icon>
      <path d="M13 3L4 14h6l-1 7 9-11h-6z" />
    </Icon>
  ),
  '/workflows': (
    <Icon>
      <rect x="3" y="4" width="7" height="6" rx="1.5" />
      <rect x="14" y="14" width="7" height="6" rx="1.5" />
      <path d="M10 7h4a3 3 0 013 3v4" />
    </Icon>
  ),
  '/jobs': (
    <Icon>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 8v4l3 2" />
    </Icon>
  ),
  '/approvals': (
    <Icon>
      <path d="M9 11l3 3 8-8" />
      <path d="M20 12v6a2 2 0 01-2 2H6a2 2 0 01-2-2V6a2 2 0 012-2h9" />
    </Icon>
  ),
  '/notifications': (
    <Icon>
      <path d="M6 9a6 6 0 1112 0c0 5 2 6 2 6H4s2-1 2-6z" />
      <path d="M10 20a2 2 0 004 0" />
    </Icon>
  ),
  '/ai': (
    <Icon>
      <path d="M12 3l2 5 5 2-5 2-2 5-2-5-5-2 5-2z" />
    </Icon>
  ),
  '/admin/users': (
    <Icon>
      <circle cx="9" cy="8" r="3" />
      <path d="M3 20a6 6 0 0112 0M16 11a3 3 0 100-6M21 20a6 6 0 00-4-5.6" />
    </Icon>
  ),
  '/admin/roles': (
    <Icon>
      <path d="M12 3l7 3v5c0 5-3 8-7 10-4-2-7-5-7-10V6z" />
      <path d="M9 12l2 2 4-4" />
    </Icon>
  ),
  '/admin/organizations': (
    <Icon>
      <path d="M4 21V6l6-3v18M10 21h10V10l-6-2" />
      <path d="M7 9h.01M7 13h.01M14 13h.01M14 17h.01" />
    </Icon>
  ),
  '/admin/teams': (
    <Icon>
      <circle cx="8" cy="9" r="2.5" />
      <circle cx="16" cy="9" r="2.5" />
      <path d="M3 19a5 5 0 0110 0M13 19a5 5 0 0110 0" />
    </Icon>
  ),
  '/admin/email': (
    <Icon>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M3 7l9 6 9-6" />
    </Icon>
  ),
  '/admin/telegram': (
    <Icon>
      <path d="M21 4L3 11l5 2 2 6 3-4 5 3z" />
    </Icon>
  ),
  '/admin/consent': (
    <Icon>
      <path d="M9 11l3 3 8-8" />
      <path d="M20 12a8 8 0 11-3-6.2" />
    </Icon>
  ),
  '/admin/sso': (
    <Icon>
      <rect x="4" y="10" width="16" height="10" rx="2" />
      <path d="M8 10V7a4 4 0 018 0v3" />
    </Icon>
  ),
  '/admin/mcp-credentials': (
    <Icon>
      <circle cx="8" cy="12" r="3.5" />
      <path d="M11 12h9M17 12v3M20 12v2" />
    </Icon>
  ),
  '/admin/configuration': (
    <Icon>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 3v2M12 19v2M3 12h2M19 12h2M6 6l1.5 1.5M16.5 16.5L18 18M18 6l-1.5 1.5M7.5 16.5L6 18" />
    </Icon>
  ),
  '/admin/audit': (
    <Icon>
      <path d="M5 4h9l5 5v11H5z" />
      <path d="M14 4v5h5M8 13h8M8 16h5" />
    </Icon>
  ),
  '/admin/security': (
    <Icon>
      <path d="M12 3l7 3v5c0 5-3 8-7 10-4-2-7-5-7-10V6z" />
      <path d="M12 8v4M12 15h.01" />
    </Icon>
  ),
  '/admin/diagnostics': (
    <Icon>
      <path d="M3 12h4l2-6 4 12 2-6h6" />
    </Icon>
  ),
}
