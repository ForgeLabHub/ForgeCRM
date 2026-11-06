/**
 * Not found, in the site's own voice.
 *
 * The CRM's `NotFoundPage` is not reused here: it renders an empty state inside
 * the application card, which inside the public layout would look like a broken
 * widget rather than a page. This one offers the three places a lost visitor is
 * most likely to want.
 */
import { Link } from 'react-router-dom'
import { PageMeta } from './components/PageMeta'

export function SiteNotFoundPage() {
  return (
    <>
      <PageMeta title="Страница не найдена" noSuffix />
      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            404
          </p>
          <h1 className="site-h1">Такой страницы нет</h1>
          <p className="site-lead">
            Возможно, адрес изменился или в ссылке опечатка. Начните с одного из разделов ниже —
            или напишите нам, и мы найдём то, что нужно.
          </p>
          <div className="site-hero__actions">
            <Link className="site-btn site-btn--primary" to="/services">
              Услуги
            </Link>
            <Link className="site-btn site-btn--secondary" to="/blog">
              Блог
            </Link>
            <Link className="site-btn site-btn--secondary" to="/contacts">
              Контакты
            </Link>
          </div>
        </div>
      </section>
      <div className="site-section" aria-hidden="true" />
    </>
  )
}
