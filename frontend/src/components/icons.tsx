/**
 * A small, dependency-free icon set.
 *
 * Drawn on a 24px grid with a single stroke weight so every glyph shares one
 * optical rhythm; a mixed bag of vendor icons is what makes a UI look assembled
 * rather than designed. Icons are decorative by default (`aria-hidden`) because
 * each one sits beside a text label — an icon that carries meaning on its own
 * takes an explicit label from the call site.
 *
 * The set is intentionally small: only the glyphs the interface actually uses.
 * An icon nobody renders is bundle weight with no reader.
 */
import type { ReactNode, SVGProps } from 'react'

export type IconProps = SVGProps<SVGSVGElement> & { size?: number }

function Icon({ size = 18, children, ...props }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...props}
    >
      {children}
    </svg>
  )
}

export const IconGrid = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="3" width="7.5" height="7.5" rx="1.5" />
    <rect x="13.5" y="3" width="7.5" height="7.5" rx="1.5" />
    <rect x="3" y="13.5" width="7.5" height="7.5" rx="1.5" />
    <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.5" />
  </Icon>
)

export const IconBuilding = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 21h18" />
    <path d="M5 21V5a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v16" />
    <path d="M15 21V9h2a2 2 0 0 1 2 2v10" />
    <path d="M9 7h2M9 11h2M9 15h2" />
  </Icon>
)

export const IconUsers = (p: IconProps) => (
  <Icon {...p}>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
    <path d="M16 3.13a4 4 0 0 1 0 7.75" />
  </Icon>
)

export const IconTarget = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <circle cx="12" cy="12" r="5" />
    <circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none" />
  </Icon>
)

export const IconKanban = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="4" width="5" height="16" rx="1.5" />
    <rect x="9.5" y="4" width="5" height="10" rx="1.5" />
    <rect x="16" y="4" width="5" height="13" rx="1.5" />
  </Icon>
)

export const IconCheckSquare = (p: IconProps) => (
  <Icon {...p}>
    <path d="M9 11l3 3L22 4" />
    <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11" />
  </Icon>
)

export const IconLifeBuoy = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <circle cx="12" cy="12" r="3.6" />
    <path d="M5.6 5.6l3.9 3.9M14.5 14.5l3.9 3.9M18.4 5.6l-3.9 3.9M9.5 14.5l-3.9 3.9" />
  </Icon>
)

export const IconChart = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 3v18h18" />
    <path d="M7 15l3.5-4 3 2.5L20 7" />
  </Icon>
)

export const IconFlask = (p: IconProps) => (
  <Icon {...p}>
    <path d="M9 3h6" />
    <path d="M10 3v6.5L5.4 17A2 2 0 0 0 7.1 20h9.8a2 2 0 0 0 1.7-3L14 9.5V3" />
    <path d="M7.5 14h9" />
  </Icon>
)

export const IconTrendingUp = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 17l6-6 4 4 8-8" />
    <path d="M15 7h6v6" />
  </Icon>
)

export const IconShieldAlert = (p: IconProps) => (
  <Icon {...p}>
    <path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z" />
    <path d="M12 9v4" />
    <path d="M12 16.5h.01" />
  </Icon>
)

export const IconDatabase = (p: IconProps) => (
  <Icon {...p}>
    <ellipse cx="12" cy="5.5" rx="8" ry="3" />
    <path d="M4 5.5v13c0 1.66 3.58 3 8 3s8-1.34 8-3v-13" />
    <path d="M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3" />
  </Icon>
)

export const IconLayers = (p: IconProps) => (
  <Icon {...p}>
    <path d="M12 3l9 5-9 5-9-5z" />
    <path d="M3 13l9 5 9-5" />
  </Icon>
)

export const IconPlug = (p: IconProps) => (
  <Icon {...p}>
    <path d="M9 3v5" />
    <path d="M15 3v5" />
    <path d="M6 8h12v2a6 6 0 0 1-6 6 6 6 0 0 1-6-6z" />
    <path d="M12 16v5" />
  </Icon>
)

export const IconWebhook = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="6" cy="18" r="3" />
    <circle cx="18" cy="6" r="3" />
    <path d="M8.2 15.8L15.8 8.2" />
    <path d="M6 15V6a2 2 0 0 1 2-2h2" />
    <path d="M18 9v9a2 2 0 0 1-2 2h-2" />
  </Icon>
)

export const IconActivity = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 12h4l3 8 4-16 3 8h4" />
  </Icon>
)

export const IconWorkflow = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="6" cy="6" r="2.5" />
    <circle cx="6" cy="18" r="2.5" />
    <circle cx="18" cy="12" r="2.5" />
    <path d="M6 8.5v7" />
    <path d="M8.3 6.9l7.4 3.7" />
    <path d="M8.3 17.1l7.4-3.7" />
  </Icon>
)

export const IconClock = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3.5 2" />
  </Icon>
)

export const IconBadgeCheck = (p: IconProps) => (
  <Icon {...p}>
    <path d="M12 3l2.3 1.6 2.8-.2 1 2.6 2.3 1.6-.9 2.7.9 2.7-2.3 1.6-1 2.6-2.8-.2L12 21l-2.3-1.6-2.8.2-1-2.6L3.6 15l.9-2.7-.9-2.7 2.3-1.6 1-2.6 2.8.2z" />
    <path d="M9 12l2 2 4-4" />
  </Icon>
)

export const IconBell = (p: IconProps) => (
  <Icon {...p}>
    <path d="M18 8a6 6 0 0 0-12 0c0 7-3 8-3 8h18s-3-1-3-8" />
    <path d="M10.3 21a2 2 0 0 0 3.4 0" />
  </Icon>
)

export const IconSparkles = (p: IconProps) => (
  <Icon {...p}>
    <path d="M12 3l1.8 4.7L18.5 9.5l-4.7 1.8L12 16l-1.8-4.7L5.5 9.5l4.7-1.8z" />
    <path d="M19 14l.9 2.3 2.3.9-2.3.9-.9 2.3-.9-2.3-2.3-.9 2.3-.9z" />
  </Icon>
)

export const IconUserCog = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M2.5 20a6.5 6.5 0 0 1 11-4.7" />
    <circle cx="17.5" cy="16.5" r="2.6" />
    <path d="M17.5 12.4v1.2M17.5 19.4v1.2M21 14.4l-1 .6M15 18l-1 .6M21 18.6l-1-.6M15 15l-1-.6" />
  </Icon>
)

export const IconKeyRound = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="8" cy="15" r="4" />
    <path d="M10.8 12.2L20 3" />
    <path d="M17 6l2 2M14.5 8.5l2 2" />
  </Icon>
)

export const IconGlobe = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M3 12h18" />
    <path d="M12 3c2.5 2.5 3.8 5.7 3.8 9s-1.3 6.5-3.8 9c-2.5-2.5-3.8-5.7-3.8-9S9.5 5.5 12 3z" />
  </Icon>
)

export const IconUsersRound = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M2.5 20a6.5 6.5 0 0 1 13 0" />
    <path d="M16 5a3.5 3.5 0 0 1 0 6.8" />
    <path d="M17.5 14.3A6.5 6.5 0 0 1 21.5 20" />
  </Icon>
)

export const IconMail = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="5" width="18" height="14" rx="2" />
    <path d="M3.5 7l8.5 6 8.5-6" />
  </Icon>
)

export const IconSend = (p: IconProps) => (
  <Icon {...p}>
    <path d="M21.5 3.5L10.5 14.5" />
    <path d="M21.5 3.5L14.5 21l-4-6.5-6.5-4z" />
  </Icon>
)

export const IconClipboardCheck = (p: IconProps) => (
  <Icon {...p}>
    <rect x="6" y="4" width="12" height="17" rx="2" />
    <path d="M9 4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2" />
    <path d="M9.5 13l2 2 3.5-4" />
  </Icon>
)

export const IconLock = (p: IconProps) => (
  <Icon {...p}>
    <rect x="4.5" y="10" width="15" height="11" rx="2" />
    <path d="M8 10V7a4 4 0 0 1 8 0v3" />
    <path d="M12 14.5v2.5" />
  </Icon>
)

export const IconTerminal = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <path d="M7 9l3 3-3 3" />
    <path d="M13 15h4" />
  </Icon>
)

export const IconSliders = (p: IconProps) => (
  <Icon {...p}>
    <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
    <circle cx="16" cy="7" r="2" />
    <circle cx="10" cy="17" r="2" />
  </Icon>
)

export const IconScroll = (p: IconProps) => (
  <Icon {...p}>
    <path d="M6 3h9l4 4v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
    <path d="M14 3v5h5" />
    <path d="M8.5 13h7M8.5 16.5h5" />
  </Icon>
)

export const IconShield = (p: IconProps) => (
  <Icon {...p}>
    <path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z" />
    <path d="M9 12l2 2 4-4" />
  </Icon>
)

export const IconStethoscope = (p: IconProps) => (
  <Icon {...p}>
    <path d="M5 3v5a4 4 0 0 0 8 0V3" />
    <path d="M9 12v3a5 5 0 0 0 5 5 5 5 0 0 0 5-5v-1" />
    <circle cx="19" cy="11" r="2" />
  </Icon>
)

export const IconUser = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21a8 8 0 0 1 16 0" />
  </Icon>
)

export const IconSearch = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="11" cy="11" r="7" />
    <path d="M20 20l-3.6-3.6" />
  </Icon>
)

export const IconMenu = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 6h18M3 12h18M3 18h18" />
  </Icon>
)

export const IconClose = (p: IconProps) => (
  <Icon {...p}>
    <path d="M6 6l12 12M18 6L6 18" />
  </Icon>
)

export const IconSun = (p: IconProps) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5L19 19M19 5l-1.5 1.5M6.5 17.5L5 19" />
  </Icon>
)

export const IconMoon = (p: IconProps) => (
  <Icon {...p}>
    <path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z" />
  </Icon>
)

export const IconMonitor = (p: IconProps) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </Icon>
)

export const IconLogOut = (p: IconProps) => (
  <Icon {...p}>
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
    <path d="M16 17l5-5-5-5" />
    <path d="M21 12H9" />
  </Icon>
)

export const IconInbox = (p: IconProps) => (
  <Icon {...p}>
    <path d="M3 12h5l1.5 3h5L16 12h5" />
    <path d="M5 5h14l2 7v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-6z" />
  </Icon>
)

export const IconChevronDown = (p: IconProps) => (
  <Icon {...p}>
    <path d="M6 9l6 6 6-6" />
  </Icon>
)
