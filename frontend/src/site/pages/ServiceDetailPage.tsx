/**
 * A single service.
 *
 * A service page answers three questions in order: what changes for the client,
 * how the work is done, and what it costs. That order is the same in every
 * variant — what changes is the density, the presence of the summary rail and
 * whether the numbers come before the prose.
 */
import { Link, useParams } from 'react-router-dom'
import { SERVICES, SERVICE_GROUPS } from '../content'
import { NotFoundPage } from '../../pages/ErrorPages'
import { PageMeta } from './components/PageMeta'

export function ServiceDetailPage() {
  const { slug } = useParams<{ slug: string }>()
  const service = SERVICES.find((item) => item.slug === slug)

  if (!service) return <NotFoundPage />

  const group = SERVICE_GROUPS.find((item) => item.id === service.group)
  const others = SERVICES.filter((item) => item.slug !== service.slug && item.group === service.group)

  return (
    <>
      <PageMeta title={service.title} description={service.summary} />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            {group?.title ?? 'Услуга'}
          </p>
          <h1 className="site-h1">{service.title}</h1>
          <p className="site-lead">{service.summary}</p>
          <div className="site-hero__actions" style={{ marginBottom: 0 }}>
            <Link className="site-btn site-btn--primary" to="/contacts">
              Обсудить проект
            </Link>
            <Link className="site-btn site-btn--secondary" to="/pricing">
              Стоимость
            </Link>
          </div>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="article__body">
            <div>
              <h2 className="site-h2">Что вы получите</h2>
              <ul className="site-tariff__features">
                {service.outcomes.map((outcome) => (
                  <li key={outcome}>{outcome}</li>
                ))}
              </ul>

              <h2 className="site-h2" style={{ marginTop: 'clamp(32px, 4vw, 48px)' }}>
                Как проходит работа
              </h2>
              <div className="site-dl">
                {service.steps.map((step, index) => (
                  <div className="site-dl__row" key={step.title}>
                    <dt>
                      {index + 1}. {step.title}
                    </dt>
                    <dd>{step.text}</dd>
                  </div>
                ))}
              </div>
            </div>

            <aside className="article__aside">
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Стоимость</p>
                <p className="site-tariff__price">
                  {service.from === null ? 'по запросу' : `от ${service.from.toLocaleString('ru-RU')} ₽`}
                </p>
                <p className="site-tariff__note">Итоговая цена — после диагностики</p>
              </div>
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Срок</p>
                <p className="site-card__price" style={{ fontSize: 17 }}>
                  {service.duration}
                </p>
                <p className="site-tariff__note">Фиксируется в договоре после диагностики</p>
              </div>
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Кто ведёт</p>
                <p className="site-card__price" style={{ fontSize: 17 }}>
                  {service.lead}
                </p>
              </div>
              <div className="article__toc">
                <p className="article__toc-title">Другие услуги</p>
                <ol>
                  {others.map((item) => (
                    <li key={item.slug}>
                      <Link to={`/services/${item.slug}`}>{item.title}</Link>
                    </li>
                  ))}
                  <li>
                    <Link to="/services">Все услуги</Link>
                  </li>
                </ol>
              </div>
            </aside>
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="prose__cta">
            <div>
              <p className="prose__cta-title">Начнём с диагностики</p>
              <p className="prose__cta-text">
                Бесплатно, 45 минут. Покажем, что даст эффект в вашем случае, и назовём точную
                стоимость до подписания договора.
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
