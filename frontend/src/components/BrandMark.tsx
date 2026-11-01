/**
 * The ForgeCRM mark.
 *
 * Drawn rather than imported so it scales cleanly and inherits the theme accent
 * without a second asset. The anvil-like glyph reads as "forge", which is the
 * product's identity; it is intentionally simple so it stays legible at 20px.
 */
export function BrandMark({ size = 28, title = 'ForgeCRM' }: { size?: number; title?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      role="img"
      aria-label={title}
      className="brand-mark"
    >
      <defs>
        <linearGradient id="forgecrm-mark" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#3a5ff0" />
          <stop offset="100%" stopColor="#6d3ce0" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill="url(#forgecrm-mark)" />
      <path
        d="M10 9.5h12v2.6h-8.7v2.6H20v2.6h-6.7v6.2H10z"
        fill="#ffffff"
        fillRule="evenodd"
      />
      <circle cx="22.4" cy="21.6" r="2.1" fill="#ffffff" opacity="0.6" />
    </svg>
  )
}
