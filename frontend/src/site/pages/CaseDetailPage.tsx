/**
 * A single case study.
 *
 * Structured as task / what we did / what changed. The task section is not
 * decoration: a case that opens with the clean end state reads as a brochure,
 * and the reader cannot tell whether their own situation is comparable.
 */
import { Link, useParams } from 'react-router-dom'
import { CASES } from '../content'
import { NotFoundPage } from '../../pages/ErrorPages'
import { PageMeta } from './components/PageMeta'

export function CaseDetailPage() {
  const { slug } = useParams<{ slug: string }>()
  const item = CASES.find((entry) => entry.slug === slug)

  if (!item) return <NotFoundPage />

  const others = CASES.filter((entry) => entry.slug !== item.slug).slice(0, 3)

  return (
    <>
      <PageMeta title={item.title} description={item.task} />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            {item.industry} · {item.client}
          </p>
          <h1 className="site-h1">{item.title}</h1>
          <p className="site-lead">{item.task}</p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-stats">
            {item.results.map((result) => (
              <div className="site-stats__item" key={result.label}>
                <span className="site-stats__value">{result.value}</span>
                <span className="site-stats__label">{result.label}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section" style={{ paddingTop: 0 }}>
        <div className="site-container">
          <div className="article__body">
            <div>
              <h2 className="site-h2">Задача</h2>
              <p className="article__p">{item.task}</p>

              <h2 className="site-h2">Что сделали</h2>
              <div className="site-dl">
                {item.solution.map((step, index) => (
                  <div className="site-dl__row" key={step}>
                    <dt>{index + 1}</dt>
                    <dd>{step}</dd>
                  </div>
                ))}
              </div>

              <h2 className="site-h2">Результат</h2>
              <p className="article__p">
                Цифры выше клиент проверял сам в отчётах системы — не мы считали их за него.
                Изменения замеряли через месяц после запуска, сравнивая с периодом до внедрения.
              </p>
              <ul className="site-tariff__features">
                {item.results.map((result) => (
                  <li key={result.label}>
                    <strong>{result.value}</strong> — {result.label}
                  </li>
                ))}
              </ul>

              <div className="prose__cta" style={{ marginTop: 40 }}>
                <div>
                  <p className="prose__cta-title">Похожая задача?</p>
                  <p className="prose__cta-text">
                    Расскажите, как устроены продажи у вас. Сверим с этим проектом и скажем, что
                    сработает, а что — нет.
                  </p>
                </div>
                <Link className="site-btn site-btn--primary" to="/contacts">
                  Обсудить
                </Link>
              </div>
            </div>

            <aside className="article__aside">
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Карточка проекта</p>
                <ul className="site-tariff__features" style={{ marginTop: 12 }}>
                  <li>Клиент: {item.client}</li>
                  <li>Отрасль: {item.industry}</li>
                  <li>Срок: {item.duration}</li>
                  <li>Стек: {item.stack.join(', ')}</li>
                </ul>
              </div>
              <div className="article__toc">
                <p className="article__toc-title">Другие кейсы</p>
                <ol>
                  {others.map((entry) => (
                    <li key={entry.slug}>
                      <Link to={`/cases/${entry.slug}`}>{entry.client}</Link>
                    </li>
                  ))}
                  <li>
                    <Link to="/cases">Все кейсы</Link>
                  </li>
                </ol>
              </div>
            </aside>
          </div>
        </div>
      </section>
    </>
  )
}
