/**
 * The case index.
 *
 * Each card carries its own headline metric, because a case list without numbers
 * is a list of client logos, and logos persuade nobody who has to justify the
 * budget to someone else.
 */
import { Link } from 'react-router-dom'
import { CASES } from '../content'
import { PageMeta } from './components/PageMeta'

export function CasesPage() {
  return (
    <>
      <PageMeta
        title="Кейсы внедрения CRM"
        description="Шесть проектов с цифрами: рост конверсии, сокращение цикла сделки, разгрузка менеджеров."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Кейсы
          </p>
          <h1 className="site-h1">Проекты и цифры</h1>
          <p className="site-lead">
            Клиенты сами называют метрики, которые проверяли. Мы не считаем «сэкономленные часы» и
            не переводим их в деньги по выдуманной ставке.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-grid site-grid--cases">
            {CASES.map((item) => (
              <Link className="site-card" to={`/cases/${item.slug}`} key={item.slug}>
                <p className="site-card__kicker">
                  {item.industry} · {item.client}
                </p>
                <h3 className="site-card__title">{item.title}</h3>
                <p className="site-card__text">{item.task}</p>
                <div className="site-card__foot">
                  {item.results.map((result) => (
                    <span key={result.label}>
                      <span className="site-metric">{result.value}</span>
                      <span className="site-metric-label">{result.label}</span>
                    </span>
                  ))}
                </div>
                <p className="site-card__price" style={{ marginTop: 14 }}>
                  Срок: {item.duration}
                </p>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="prose__cta">
            <div>
              <p className="prose__cta-title">Хотите похожий результат?</p>
              <p className="prose__cta-text">
                Начните с бесплатной диагностики. За 45 минут поймём, где сейчас теряются заявки, и
                что менять первым.
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
