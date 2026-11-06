/**
 * The services catalogue.
 *
 * Grouped by direction rather than listed alphabetically: a visitor arrives with
 * a problem ("у нас Битрикс24 не приживается"), not with the name of a service,
 * so the groups are the primary navigation and the services are their contents.
 */
import { Link } from 'react-router-dom'
import { SERVICE_GROUPS, SERVICES } from '../content'
import { PageMeta } from './components/PageMeta'

export function ServicesPage() {
  return (
    <>
      <PageMeta
        title="Услуги: внедрение, настройка, разработка и поддержка CRM"
        description="Внедрение amoCRM и Битрикс24, разработка собственной CRM, аудит, поддержка и обучение. Цены и сроки."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Услуги
          </p>
          <h1 className="site-h1">Что мы делаем</h1>
          <p className="site-lead">
            Восемь услуг в шести направлениях. Каждая начинается с разбора процесса и заканчивается
            измеримым результатом, а не актом приёмки.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          {SERVICE_GROUPS.map((group, index) => {
            const items = SERVICES.filter((service) => service.group === group.id)
            if (items.length === 0) return null
            return (
              <div
                key={group.id}
                style={{ marginTop: index === 0 ? 0 : 'clamp(40px, 5vw, 64px)' }}
              >
                <div className="site-section__head">
                  <div>
                    <h2 className="site-h2 site-h2--tight">{group.title}</h2>
                    <p className="site-section__lede" style={{ marginTop: 8 }}>
                      {group.blurb}
                    </p>
                  </div>
                </div>
                <div className="site-grid site-grid--services">
                  {items.map((service) => (
                    <Link className="site-card" to={`/services/${service.slug}`} key={service.slug}>
                      <h3 className="site-card__title">{service.title}</h3>
                      <p className="site-card__text">{service.summary}</p>
                      <div className="site-card__foot">
                        <span className="site-card__price">
                          {service.from === null
                            ? 'по запросу'
                            : `от ${service.from.toLocaleString('ru-RU')} ₽`}
                        </span>
                        <span>{service.duration}</span>
                      </div>
                    </Link>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="prose__cta">
            <div>
              <p className="prose__cta-title">Нужна помощь с выбором?</p>
              <p className="prose__cta-text">
                Расскажите, как у вас устроены продажи сейчас. Мы скажем, что даст эффект в первую
                очередь, и назовём стоимость до начала работ.
              </p>
            </div>
            <Link className="site-btn site-btn--primary" to="/contacts">
              Обсудить задачу
            </Link>
          </div>
        </div>
      </section>
    </>
  )
}
