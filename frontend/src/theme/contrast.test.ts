/**
 * Design-token contrast.
 *
 * The colour tokens are the one part of the design system that no component test
 * can catch: a token can be changed to a value that looks fine and quietly drops
 * body text below the WCAG AA threshold everywhere at once. This reads the real
 * stylesheet and checks the text/background pairs the interface actually uses, so
 * the guarantee is enforced by the same file the browser renders.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(__dirname, '../index.css'), 'utf8')

/** Extracts a `--name: #hex;` declaration from the first block that matches. */
function token(name: string, occurrence = 0): string {
  const matches = [...css.matchAll(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`, 'g'))]
  const value = matches[occurrence]?.[1]
  if (!value) throw new Error(`token --${name} (occurrence ${occurrence}) not found in index.css`)
  return value
}

function channel(value: number): number {
  const c = value / 255
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16))
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

function contrast(foreground: string, background: string): number {
  const [hi, lo] = [luminance(foreground), luminance(background)].sort((a, b) => b - a)
  return (hi + 0.05) / (lo + 0.05)
}

// The light palette is the first declaration of each token; the dark palette is
// declared twice (attribute selector and media query) with identical values, so
// the second occurrence is the dark one.
const LIGHT = {
  surface: token('color-surface', 0),
  bg: token('color-bg', 0),
  text: token('color-text', 0),
  muted: token('color-text-muted', 0),
  subtle: token('color-text-subtle', 0),
  accent: token('color-accent', 0),
  accentSoft: token('color-accent-soft', 0),
  onAccent: token('color-on-accent', 0),
}

const DARK = {
  surface: token('color-surface', 1),
  bg: token('color-bg', 1),
  text: token('color-text', 1),
  muted: token('color-text-muted', 1),
  subtle: token('color-text-subtle', 1),
  accent: token('color-accent', 1),
  accentSoft: token('color-accent-soft', 1),
  onAccent: token('color-on-accent', 1),
}

const AA_NORMAL = 4.5
const AA_LARGE = 3

describe.each([
  ['light', LIGHT],
  ['dark', DARK],
])('%s theme contrast', (_name, palette) => {
  it('renders primary text at AA or better on both surfaces', () => {
    expect(contrast(palette.text, palette.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(contrast(palette.text, palette.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('renders muted text at AA or better on both surfaces', () => {
    expect(contrast(palette.muted, palette.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(contrast(palette.muted, palette.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  // Subtle text is small hint copy, so it must clear the normal-text threshold,
  // not the large-text one. This is the regression the token fix addressed.
  it('renders subtle hint text at AA or better on both surfaces', () => {
    expect(contrast(palette.subtle, palette.surface)).toBeGreaterThanOrEqual(AA_NORMAL)
    expect(contrast(palette.subtle, palette.bg)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('keeps the primary button label readable on the accent fill', () => {
    expect(contrast(palette.onAccent, palette.accent)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('keeps accent text readable when used as a link or icon colour', () => {
    expect(contrast(palette.accent, palette.surface)).toBeGreaterThanOrEqual(AA_LARGE)
    expect(contrast(palette.accent, palette.bg)).toBeGreaterThanOrEqual(AA_LARGE)
  })

  // The active nav row and the info alert render accent-coloured text on the
  // accent-soft fill. That pair is normal-sized text, so it must clear AA_NORMAL,
  // not AA_LARGE — the gap the earlier light-theme value (#2563eb) slipped through
  // at 4.485:1.
  it('keeps accent text readable on the accent-soft fill', () => {
    expect(contrast(palette.accent, palette.accentSoft)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  // The search palette renders a subtle type label on an active row, whose
  // background is accent-soft. Same threshold as any other small hint text.
  it('keeps subtle hint text readable on the accent-soft fill', () => {
    expect(contrast(palette.subtle, palette.accentSoft)).toBeGreaterThanOrEqual(AA_NORMAL)
  })
})
