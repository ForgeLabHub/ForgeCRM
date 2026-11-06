/**
 * Pricing and licences.
 *
 * Two things a buyer compares: what the engagement costs and what the platform
 * licence costs. They are kept apart because they are bought from different
 * budgets and renew on different schedules, and mixing them into one table is
 * how a "from" price stops meaning anything.
 */
import { Link } from 'react-router-dom'
import { LICENSES, PROMO, SERVICES, TARIFFS } from '../content'
import { PageMeta } from './components/PageMeta'

export function PricingPage() {
  const courses = SERVICES.filter((service) => service.group === 'courses')

  return (
    <>
      <PageMeta
        title="Тарифы, лицензии и стоимость"
        description="Стоимость внедрения CRM, тарифы на поддержку и лицензии amoCRM и Битрикс24 со скидкой до 30%."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Прозрачные цены
          </p>
          <h1 className="site-h1">Тарифы и лицензии</h1>
          <p className="site-lead">
            Цена внедрения зависит от числа воронок и интеграций, поэтому мы называем «от» и
            уточняем после диагностики. Диагностика бесплатная — чтобы вы платили за работу, а не
            за право узнать стоимость.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-tariffs">
            {TARIFFS.map((tariff) => (
              <div
                className={tariff.featured ? 'site-tariff site-tariff--featured' : 'site-tariff'}
                key={tariff.name}
              >
                {tariff.featured ? <span className="site-tariff__badge">Выбирают чаще</span> : null}
                <p className="site-tariff__name">{tariff.name}</p>
                <p className="site-tariff__price">{tariff.price}</p>
                <p className="site-tariff__note">{tariff.note}</p>
                <ul className="site-tariff__features">
                  {tariff.features.map((feature) => (
                    <li key={feature}>{feature}</li>
                  ))}
                </ul>
                <div className="site-tariff__cta">
                  <Link
                    className={
                      tariff.featured
                        ? 'site-btn site-btn--primary site-btn--block'
                        : 'site-btn site-btn--secondary site-btn--block'
                    }
                    to="/contacts"
                  >
                    {tariff.name === 'Диагностика' ? 'Записаться' : 'Обсудить проект'}
                  </Link>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Лицензии</h2>
              <p className="site-section__lede">
                Продаём лицензии как партнёр вендора. Скидка действует при внедрении и на продление
                — <strong>{PROMO.discount}</strong> и {PROMO.gift}.
              </p>
            </div>
          </div>
          <div className="site-grid site-grid--three">
            {LICENSES.map((licence) => (
              <div className="site-card site-card--plain" key={licence.title}>
                <p className="site-card__kicker">{licence.vendor}</p>
                <h3 className="site-card__title">{licence.title}</h3>
                <p className="site-card__text">{licence.text}</p>
                <div className="site-card__foot">
                  <span className="site-card__price">{licence.discount}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Обучение</h2>
              <p className="site-section__lede">
                Курсы для тех, кто будет вести систему внутри компании. Дешевле, чем каждый раз
                звать подрядчика из-за новой воронки.
              </p>
            </div>
          </div>
          <div className="site-grid site-grid--three">
            {courses.map((course) => (
              <Link className="site-card" to={`/services/${course.slug}`} key={course.slug}>
                <h3 className="site-card__title">{course.title}</h3>
                <p className="site-card__text">{course.summary}</p>
                <div className="site-card__foot">
                  <span className="site-card__price">
                    {course.from === null
                      ? 'по запросу'
                      : `от ${course.from.toLocaleString('ru-RU')} ₽`}
                  </span>
                  <span>{course.duration}</span>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="site-dl">
            <div className="site-dl__row">
              <dt>Что входит в «от»</dt>
              <dd>
                Диагностика, проектирование воронок, настройка, перенос базы и обучение команды.
                Дополнительно оплачиваются только лицензии и сторонние сервисы.
              </dd>
            </div>
            <div className="site-dl__row">
              <dt>Можно ли частями</dt>
              <dd>
                Да: этапами — диагностика и проектирование, затем настройка, затем запуск. Каждый
                этап принимается отдельно, поэтому можно остановиться без потери результата.
              </dd>
            </div>
            <div className="site-dl__row">
              <dt>Что если не окупится</dt>
              <dd>
                Через месяц после запуска сверяем цифры из отчётов с исходными. Если изменений нет,
                разбираемся, почему, и доводим настройку без доплаты.
              </dd>
            </div>
          </div>
        </div>
      </section>
    </>
  )
}
