/**
 * Contrast for the ten visual styles.
 *
 * The default palette is checked by contrast.test.ts. The ten named styles each
 * declare their own colours, and a style is exactly the kind of change that can
 * look fine and quietly drop body text below the WCAG AA threshold everywhere at
 * once. This reads the real stylesheet, parses every `:root[data-style='…']`
 * block, and checks the foreground/background pairs the interface actually
 * renders, so the guarantee is enforced by the same file the browser uses.
 *
 * Translucent tokens (the Glass style) are composited over the backdrop they sit
 * on before the ratio is computed, because that is what the eye sees.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { SELECTABLE_STYLES, type ThemeStyle } from './themes'

const css = readFileSync(resolve(__dirname, '../index.css'), 'utf8')

/** Returns the declarations of one style block, keyed by custom property name. */
function styleTokens(style: ThemeStyle): Map<string, string> {
  // Match the block that opens with the style attribute and ends at the first
  // closing brace at column 0. Nested blocks are not used for the token sets.
  const re = new RegExp(`:root\\[data-style='${style}'\\]\\s*\\{([\\s\\S]*?)\\n\\}`)
  const match = css.match(re)
  if (!match) throw new Error(`style block for '${style}' not found in index.css`)
  const tokens = new Map<string, string>()
  for (const line of match[1].split('\n')) {
    const m = line.match(/(--[a-z0-9-]+):\s*([^;]+);/)
    if (m) tokens.set(m[1], m[2].trim())
  }
  return tokens
}

interface RGB {
  r: number
  g: number
  b: number
  a: number
}

/** Parses a hex or rgb(a) colour into components. */
function parse(color: string): RGB {
  const hex = color.match(/^#([0-9a-fA-F]{6})$/)
  if (hex) {
    const h = hex[1]
    return {
      r: parseInt(h.slice(0, 2), 16),
      g: parseInt(h.slice(2, 4), 16),
      b: parseInt(h.slice(4, 6), 16),
      a: 1,
    }
  }
  const rgba = color.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)\s*(?:,\s*([\d.]+)\s*)?\)$/)
  if (rgba) {
    return {
      r: Number(rgba[1]),
      g: Number(rgba[2]),
      b: Number(rgba[3]),
      a: rgba[4] === undefined ? 1 : Number(rgba[4]),
    }
  }
  throw new Error(`unsupported colour: ${color}`)
}

/** Composites a possibly translucent colour over an opaque backdrop. */
function over(fg: RGB, bg: RGB): RGB {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  }
}

function channel(value: number): number {
  const c = value / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

function luminance(c: RGB): number {
  return 0.2126 * channel(c.r) + 0.7152 * channel(c.g) + 0.0722 * channel(c.b)
}

function ratio(fg: RGB, bg: RGB): number {
  const [hi, lo] = [luminance(fg), luminance(bg)].sort((a, b) => b - a)
  return (hi + 0.05) / (lo + 0.05)
}

const AA_NORMAL = 4.5
const AA_LARGE = 3

interface Palette {
  text: RGB
  muted: RGB
  subtle: RGB
  accent: RGB
  accentSoft: RGB
  onAccent: RGB
  bg: RGB
  surface: RGB
}

function palette(style: ThemeStyle): Palette {
  const t = styleTokens(style)
  const get = (name: string): RGB => {
    const value = t.get(`--${name}`)
    if (!value) throw new Error(`--${name} missing from style '${style}'`)
    return parse(value)
  }
  // bg and surface are the opaque backdrops; everything else may be translucent
  // and is composited over them, matching how the stylesheet layers the tokens.
  const bg = get('color-bg')
  const surface = over(get('color-surface'), bg)
  const flat = (name: string): RGB => over(get(name), surface)
  return {
    bg,
    surface,
    text: flat('color-text'),
    muted: flat('color-text-muted'),
    subtle: flat('color-text-subtle'),
    accent: flat('color-accent'),
    accentSoft: over(get('color-accent-soft'), surface),
    onAccent: over(get('color-on-accent'), flat('color-accent')),
  }
}

describe.each(SELECTABLE_STYLES)('%s style contrast', (style) => {
  const p = palette(style)

  it('renders primary text at AA or better on both surfaces', () => {
    expect(ratio(p.text, p.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(ratio(p.text, p.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('renders muted text at AA or better on both surfaces', () => {
    expect(ratio(p.muted, p.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(ratio(p.muted, p.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('renders subtle hint text at AA or better on both surfaces', () => {
    expect(ratio(p.subtle, p.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(ratio(p.subtle, p.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('keeps the primary button label readable on the accent fill', () => {
    expect(ratio(p.onAccent, p.accent)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('keeps accent text readable as a link or icon colour', () => {
    expect(ratio(p.accent, p.surface)).toBeGreaterThanOrEqual(AA_LARGE)
    expect(ratio(p.accent, p.bg)).toBeGreaterThanOrEqual(AA_LARGE)
  })

  it('keeps accent text readable on the accent-soft fill', () => {
    expect(ratio(p.accent, p.accentSoft)).toBeGreaterThanOrEqual(AA_NORMAL)
  })
})
