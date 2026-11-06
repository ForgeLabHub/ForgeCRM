/**
 * The home page.
 *
 * One DOM, three arrangements. The hero, the pipeline illustration, the service
 * grid and the proof blocks are the same elements in all three variants; the
 * stylesheet decides whether the catalogue is dense, the article covers are
 * large, or the whole thing reads like a documentation index.
 */
import { Link } from 'react-router-dom'
import { ARTICLES } from '../articles'
import { CASES, CONTACTS, FAQ, INDUSTRIES, STATS, SERVICES } from '../content'
import { PageMeta } from './components/PageMeta'

function PipelineShot() {
  return (
    <div className="site-shot" aria-hidden="true">
      <div className="site-shot__bar">
        <span className="site-shot__dot" />
        <span className="site-shot__dot" />
        <span className="site-shot__dot" />
        <span className="site-shot__title">Сделки · Воронка «Производство»</span>
      </div>
      <div className="site-shot__body">
        <div className="site-shot__rail">
          <i />
          <i />
          <i />
          <i />
          <i />
        </div>
        <div className="site-shot__pipeline">
          <div className="site-shot__col">
            <span className="site-shot__col-head" />
            <div className="site-shot__card">
              <strong>ООО «Термопласт»</strong>
              <span />
              <span style={{ width: '62%' }} />
            </div>
            <div className="site-shot__card">
              <strong>«СтройКонтур»</strong>
              <span />
              <span style={{ width: '48%' }} />
            </div>
          </div>
          <div className="site-shot__col">
            <span className="site-shot__col-head" />
            <div className="site-shot__card site-shot__card--accent">
              <strong>«ЛогистПро»</strong>
              <span />
              <span style={{ width: '70%' }} />
            </div>
          </div>
          <div className="site-shot__col">
            <span className="site-shot__col-head" />
            <div className="site-shot__card">
              <strong>«ОптВектор»</strong>
              <span />
              <span style={{ width: '54%' }} />
            </div>
            <div className="site-shot__card">
              <strong>«ТендерПлюс»</strong>
              <span />
              <span style={{ width: '40%' }} />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export function HomePage() {
  const featured = SERVICES.slice(0, 6)
  const latest = ARTICLES
  const topCases = CASES.slice(0, 3)

  return (
    <>
      <PageMeta
        title="Pinscher CRM — внедрение amoCRM и Битрикс24"
        description="Внедряем amoCRM и Битрикс24, разрабатываем собственные CRM и учим команды работать в них. Диагностика бесплатно."
      />

      <section className="site-hero">
        <div className="site-container site-hero__grid">
          <div>
            <p className="site-eyebrow">
              <span className="site-eyebrow__dot" aria-hidden="true" />
              {CONTACTS.brand} · внедрение с 2011 года
            </p>
            <h1 className="site-h1">
              CRM, в которой команда <em>действительно</em> работает
            </h1>
            <p className="site-lead">
              Настраиваем amoCRM и Битрикс24 под ваш процесс продаж, а не под чужой шаблон.
              Начинаем с диагностики: находим, где теряются заявки, и только потом внедряем.
            </p>
            <div className="site-hero__actions">
              <Link className="site-btn site-btn--primary" to="/contacts">
                Бесплатная диагностика
              </Link>
              <Link className="site-btn site-btn--secondary" to="/cases">
                Смотреть кейсы
              </Link>
            </div>
            <ul className="site-hero__trust">
              <li>310+ внедрений</li>
              <li>Партнёр amoCRM и Битрикс24</li>
              <li>Фиксированный срок в договоре</li>
            </ul>
          </div>
          <PipelineShot />
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-stats">
            {STATS.map((stat) => (
              <div className="site-stats__item" key={stat.label}>
                <span className="site-stats__value">{stat.value}</span>
                <span className="site-stats__label">{stat.label}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken" id="services">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Услуги</h2>
              <p className="site-section__lede">
                Шесть направлений, которые закрывают весь путь: от выбора системы до поддержки
                после запуска.
              </p>
            </div>
            <Link className="site-section__more" to="/services">
              Все услуги →
            </Link>
          </div>
          <div className="site-grid site-grid--services">
            {featured.map((service) => (
              <Link className="site-card" to={`/services/${service.slug}`} key={service.slug}>
                <p className="site-card__kicker">{service.group === 'courses' ? 'Обучение' : 'Внедрение'}</p>
                <h3 className="site-card__title">{service.title}</h3>
                <p className="site-card__text">{service.summary}</p>
                <div className="site-card__foot">
                  <span className="site-card__price">
                    {service.from === null ? 'по запросу' : `от ${service.from.toLocaleString('ru-RU')} ₽`}
                  </span>
                  <span className="site-card__arrow">Подробнее →</span>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Отрасли</h2>
              <p className="site-section__lede">
                Готовые конфигурации для шести ниш. Если вашей нет — соберём с нуля на диагностике.
              </p>
            </div>
          </div>
          <div className="site-grid site-grid--industries">
            {INDUSTRIES.map((industry) => (
              <div className="site-card site-card--plain" key={industry.title}>
                <h3 className="site-card__title">{industry.title}</h3>
                <p className="site-card__text">{industry.text}</p>
                <div className="site-card__foot">
                  <span className="site-card__price">{industry.metric}</span>
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
              <h2 className="site-h2">Кейсы</h2>
              <p className="site-section__lede">
                Цифры, которые клиент подтвердил через месяц после запуска.
              </p>
            </div>
            <Link className="site-section__more" to="/cases">
              Все кейсы →
            </Link>
          </div>
          <div className="site-grid site-grid--cases">
            {topCases.map((item) => (
              <Link className="site-card" to={`/cases/${item.slug}`} key={item.slug}>
                <p className="site-card__kicker">{item.industry}</p>
                <h3 className="site-card__title">{item.title}</h3>
                <p className="site-card__text">{item.task}</p>
                <div className="site-card__foot">
                  {item.results.slice(0, 2).map((result) => (
                    <span key={result.label}>
                      <span className="site-metric">{result.value}</span>
                      <span className="site-metric-label">{result.label}</span>
                    </span>
                  ))}
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Блог</h2>
              <p className="site-section__lede">
                Разбираем, как выбирать CRM и что делать, когда система уже есть, но ей не
                пользуются.
              </p>
            </div>
            <Link className="site-section__more" to="/blog">
              Все статьи →
            </Link>
          </div>
          <div className="blog-list">
            {latest.map((article) => (
              <Link className="blog-item" to={`/blog/${article.slug}`} key={article.slug}>
                <span className="blog-item__thumb" aria-hidden="true" />
                <div>
                  <div className="article__meta">
                    <span className="article__tag">{article.category}</span>
                    <span>{article.date}</span>
                    <span>{article.readingTime}</span>
                  </div>
                  <h3 className="blog-item__title">{article.title}</h3>
                  <p className="blog-item__text">{article.standfirst}</p>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="site-section__head">
            <div>
              <h2 className="site-h2">Частые вопросы</h2>
            </div>
          </div>
          <div className="site-dl">
            {FAQ.map((item) => (
              <div className="site-dl__row" key={item.q}>
                <dt>{item.q}</dt>
                <dd>{item.a}</dd>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="prose__cta">
            <div>
              <p className="prose__cta-title">Не знаете, с чего начать?</p>
              <p className="prose__cta-text">
                45 минут разговора о вашем процессе продаж. Три идеи автоматизации и честный ответ
                на вопрос, нужна ли вам новая CRM.
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
