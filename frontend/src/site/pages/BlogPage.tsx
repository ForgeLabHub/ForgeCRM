/**
 * The article index.
 *
 * Sorted newest first by the date field in the content model. Categories are
 * shown as plain labels rather than as filters: with two articles a filter row
 * would be a control that mostly does nothing, and a control that does nothing
 * is worse than no control.
 */
import { Link } from 'react-router-dom'
import { ARTICLES } from '../articles'
import { PageMeta } from './components/PageMeta'

export function BlogPage() {
  return (
    <>
      <PageMeta
        title="Блог о CRM"
        description="Как выбирать CRM, что делать с существующей системой и почему команды перестают ею пользоваться."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Блог
          </p>
          <h1 className="site-h1">Разборы без воды</h1>
          <p className="site-lead">
            Пишем о том, что видим на проектах: почему внедрение не приживается, как считать эффект
            и что спрашивать у подрядчика до подписания договора.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="blog-list">
            {ARTICLES.map((article) => (
              <Link className="blog-item" to={`/blog/${article.slug}`} key={article.slug}>
                <span className="blog-item__thumb" aria-hidden="true" />
                <div>
                  <div className="article__meta">
                    <span className="article__tag">{article.category}</span>
                    <span>{article.date}</span>
                    <span>{article.readingTime}</span>
                  </div>
                  <h2 className="blog-item__title">{article.title}</h2>
                  <p className="blog-item__text">{article.standfirst}</p>
                  <p className="blog-item__text" style={{ marginTop: 8 }}>
                    {article.author.name} · {article.author.role}
                  </p>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="prose__cta">
            <div>
              <p className="prose__cta-title">Разобрать вашу ситуацию</p>
              <p className="prose__cta-text">
                Статьи общие, а процессы у всех разные. На диагностике смотрим именно ваш случай.
              </p>
            </div>
            <Link className="site-btn site-btn--primary" to="/contacts">
              Записаться
            </Link>
          </div>
        </div>
      </section>
    </>
  )
}
