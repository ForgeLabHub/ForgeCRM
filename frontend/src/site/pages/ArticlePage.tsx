/**
 * A single article.
 *
 * The body comes from the structured block model in `../articles`, not from
 * HTML strings, so headings can generate the outline, links get real router
 * navigation, and the same content can be rendered at different densities by the
 * variant stylesheet.
 */
import { Link, useParams } from 'react-router-dom'
import { articleBySlug, ARTICLES } from '../articles'
import { Blocks, articleOutline } from '../components/Blocks'
import { NotFoundPage } from '../../pages/ErrorPages'
import { PageMeta } from './components/PageMeta'

export function ArticlePage() {
  const { slug } = useParams<{ slug: string }>()
  const article = slug ? articleBySlug(slug) : undefined

  if (!article) return <NotFoundPage />

  const outline = articleOutline(article.blocks)
  const related = ARTICLES.filter((item) => item.slug !== article.slug).slice(0, 2)

  return (
    <>
      <PageMeta title={article.title} description={article.standfirst} />

      <section className="blog-head">
        <div className="site-container">
          <div className="article__meta">
            <span className="article__tag">{article.category}</span>
            <span>{article.date}</span>
            <span>{article.readingTime}</span>
          </div>
          <h1 className="site-h1">{article.title}</h1>
          <p className="site-lead">{article.standfirst}</p>
        </div>
      </section>

      <section className="site-section" style={{ paddingTop: 0 }}>
        <div className="site-container">
          <div className="article__body">
            <article className="article">
              <Blocks blocks={article.blocks} />
              <div className="prose__cta" style={{ marginTop: 40 }}>
                <div>
                  <p className="prose__cta-title">Нужна помощь с выбором?</p>
                  <p className="prose__cta-text">
                    Бесплатная диагностика: 45 минут про ваш процесс продаж и конкретные
                    рекомендации.
                  </p>
                </div>
                <Link className="site-btn site-btn--primary" to="/contacts">
                  Записаться
                </Link>
              </div>
            </article>

            <aside className="article__aside">
              {outline.length > 0 ? (
                <div className="article__toc">
                  <p className="article__toc-title">Содержание</p>
                  <ol>
                    {outline.map((heading) => (
                      <li key={heading.id}>
                        <a href={`#${heading.id}`}>{heading.text}</a>
                      </li>
                    ))}
                  </ol>
                </div>
              ) : null}

              <div className="article__author">
                <span className="article__author-avatar" aria-hidden="true">
                  {article.author.initials}
                </span>
                <div>
                  <p className="article__author-name">{article.author.name}</p>
                  <p className="article__author-role">{article.author.role}</p>
                </div>
              </div>

              {related.length > 0 ? (
                <div className="article__toc">
                  <p className="article__toc-title">Читайте также</p>
                  <ol>
                    {related.map((item) => (
                      <li key={item.slug}>
                        <Link to={`/blog/${item.slug}`}>{item.title}</Link>
                      </li>
                    ))}
                  </ol>
                </div>
              ) : null}
            </aside>
          </div>
        </div>
      </section>
    </>
  )
}
