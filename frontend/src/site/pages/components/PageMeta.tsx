/**
 * Per-page document metadata.
 *
 * The application is a single-page app, so the document title and description
 * do not change on navigation unless something changes them. A public site is
 * the one place where that matters for real: a shared link preview and the
 * browser tab both read these, and every page having the application's title is
 * how a site ends up indistinguishable in a list of open tabs.
 *
 * Rendered as a component so the values are typed and colocated with the page,
 * and cleaned up on unmount so the CRM's own title is restored when the visitor
 * signs in.
 */
import { useEffect } from 'react'

const SUFFIX = 'Pinscher CRM'

export function PageMeta({
  title,
  description,
  noSuffix = false,
}: {
  title: string
  /** Omitted for pages where the site-level description is the right one. */
  description?: string
  /** Use when the title already carries the brand, e.g. the home page. */
  noSuffix?: boolean
}) {
  useEffect(() => {
    const previousTitle = document.title
    const previousDescription = document
      .querySelector('meta[name="description"]')
      ?.getAttribute('content')

    document.title = noSuffix ? title : `${title} — ${SUFFIX}`
    if (description) {
      let meta = document.querySelector('meta[name="description"]')
      if (!meta) {
        meta = document.createElement('meta')
        meta.setAttribute('name', 'description')
        document.head.appendChild(meta)
      }
      meta.setAttribute('content', description)
    }

    return () => {
      document.title = previousTitle
      const meta = document.querySelector('meta[name="description"]')
      if (meta && previousDescription !== null && previousDescription !== undefined) {
        meta.setAttribute('content', previousDescription)
      }
    }
  }, [title, description, noSuffix])

  return null
}
