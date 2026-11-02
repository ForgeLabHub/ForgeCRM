/**
 * Design-token references.
 *
 * A component that reads a custom property the stylesheet never defines gets an
 * empty value: the declaration is dropped and the element silently falls back to
 * whatever it inherited. That is how a hardcoded light fallback survived in the
 * dark theme and how a font size quietly stopped applying. This walks every
 * source file and fails when a `var(--token)` has no matching definition.
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const srcDir = resolve(__dirname, '..')
const cssPath = join(srcDir, 'index.css')

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) return walk(full)
    return /\.(ts|tsx|css)$/.test(full) && !/\.test\./.test(full) ? [full] : []
  })
}

const css = readFileSync(cssPath, 'utf8')
const defined = new Set(css.match(/--[a-z0-9-]+(?=\s*:)/g) ?? [])

describe('design tokens', () => {
  it('defines every custom property a source file references', () => {
    const missing = new Map<string, string[]>()
    for (const file of walk(srcDir)) {
      const text = readFileSync(file, 'utf8')
      for (const token of text.match(/var\((--[a-z0-9-]+)/g) ?? []) {
        const name = token.slice('var('.length)
        if (defined.has(name)) continue
        const where = file.replace(`${srcDir}/`, '')
        missing.set(name, [...(missing.get(name) ?? []), where])
      }
    }
    expect([...missing.entries()]).toEqual([])
  })
})
