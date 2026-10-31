/**
 * The ForgeCRM mark.
 *
 * Drawn rather than imported so it scales cleanly and inherits the theme accent
 * without a second asset. The anvil-like glyph reads as "forge", which is the
 * product's identity; it is intentionally simple so it stays legible at 20px.
 */
export function BrandMark({ size = 28 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      role="img"
      aria-label="ForgeCRM"
      className="brand-mark"
    >
      <rect width="32" height="32" rx="8" fill="var(--color-accent)" />
      <path
        d="M10 9.5h12v2.6h-8.7v2.6H20v2.6h-6.7v6.2H10z"
        fill="#fff"
        fillRule="evenodd"
      />
      <circle cx="22.4" cy="21.6" r="2.1" fill="#fff" opacity="0.55" />
    </svg>
  )
}
