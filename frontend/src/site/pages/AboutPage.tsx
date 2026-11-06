/**
 * About the company.
 *
 * Written as a page of facts rather than a mission statement: years, people,
 * partners, and an explicit list of what we do not do. The last item earns more
 * trust than any adjective, because every agency claims the first three.
 */
import { Link } from 'react-router-dom'
import { CONTACTS, STATS } from '../content'
import { PageMeta } from './components/PageMeta'

const PRINCIPLES = [
  {
    title: 'Сначала диагностика, потом договор',
    text: 'Мы не называем цену за внедрение до того, как увидели процесс. Смета «по телефону» — это или завышение на всякий случай, или догадка, которая всплывёт актом на допработы.',
  },
  {
    title: 'Настраиваем под процесс, а не под шаблон',
    text: 'Отраслевые конфигурации у нас есть, но мы не ставим их «как у всех». Если вендорская логика противоречит вашей — меняем настройку, а команду не заставляем подстраиваться.',
  },
  {
    title: 'Обучаем, а не сдаём',
    text: 'Внедрение считается законченным, когда менеджеры работают в системе без напоминаний. Поэтому обучение входит в стоимость, а не продаётся отдельно.',
  },
  {
    title: 'Говорим «не нужно»',
    text: 'Иногда задача решается порядком в почте, а не CRM. В таком случае мы так и пишем — и остаёмся в списке подрядчиков на будущее.',
  },
]

export function AboutPage() {
  return (
    <>
      <PageMeta
        title="О компании"
        description="Внедряем CRM с 2011 года: 310+ проектов, команда из 18 человек, партнёр amoCRM и Битрикс24."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            О нас
          </p>
          <h1 className="site-h1">{CONTACTS.brand}</h1>
          <p className="site-lead">
            Команда из 18 человек, которая с 2011 года занимается одним делом: делает так, чтобы
            CRM в компании работала, а не числилась вложениями.
          </p>
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

      <section className="site-section" style={{ paddingTop: 0 }}>
        <div className="site-container">
          <div className="article__body">
            <div>
              <h2 className="site-h2">Как мы работаем</h2>
              <div className="site-dl">
                {PRINCIPLES.map((item) => (
                  <div className="site-dl__row" key={item.title}>
                    <dt>{item.title}</dt>
                    <dd>{item.text}</dd>
                  </div>
                ))}
              </div>

              <h2 className="site-h2" style={{ marginTop: 'clamp(32px, 4vw, 48px)' }}>
                Чем не занимаемся
              </h2>
              <ul className="site-tariff__features">
                <li>Не продаём «CRM под ключ» без анализа процесса — так не бывает</li>
                <li>Не берём проекты с обещанным сроком до диагностики</li>
                <li>Не прячем допработы в актах: объём и цена фиксируются до старта</li>
                <li>Не оставляем клиента с системой, которую некому поддерживать</li>
              </ul>

              <div className="prose__cta" style={{ marginTop: 40 }}>
                <div>
                  <p className="prose__cta-title">Приезжайте знакомиться</p>
                  <p className="prose__cta-text">
                    {CONTACTS.address}. Кофе и разбор вашего процесса — за наш счёт.
                  </p>
                </div>
                <Link className="site-btn site-btn--primary" to="/contacts">
                  Связаться
                </Link>
              </div>
            </div>

            <aside className="article__aside">
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Реквизиты</p>
                <ul className="site-tariff__features" style={{ marginTop: 12 }}>
                  <li>{CONTACTS.legal}</li>
                  <li>{CONTACTS.phone}</li>
                  <li>{CONTACTS.email}</li>
                  <li>{CONTACTS.hours}</li>
                </ul>
              </div>
              <div className="article__toc">
                <p className="article__toc-title">Дальше</p>
                <ol>
                  <li>
                    <Link to="/services">Услуги</Link>
                  </li>
                  <li>
                    <Link to="/cases">Кейсы</Link>
                  </li>
                  <li>
                    <Link to="/pricing">Тарифы</Link>
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
