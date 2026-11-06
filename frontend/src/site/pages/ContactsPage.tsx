/**
 * Contacts.
 *
 * The form posts nowhere. That is deliberate and honest: there is no public
 * lead endpoint in the backend, and inventing a fetch to a route that does not
 * exist would show the visitor a success message that never reached anyone.
 * Instead the form composes a message and hands it to the visitor's own mail
 * client, and the phone and Telegram links are the primary path.
 */
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { CONTACTS, FAQ } from '../content'
import { PageMeta } from './components/PageMeta'

const TOPICS = [
  'Внедрение amoCRM',
  'Настройка Битрикс24',
  'Разработка своей CRM',
  'Аудит текущей системы',
  'Поддержка',
  'Обучение',
  'Другое',
]

export function ContactsPage() {
  const [topic, setTopic] = useState(TOPICS[0])
  const [name, setName] = useState('')
  const [company, setCompany] = useState('')
  const [message, setMessage] = useState('')

  const subject = `Заявка с сайта: ${topic}`
  const body = [
    `Тема: ${topic}`,
    name ? `Имя: ${name}` : '',
    company ? `Компания: ${company}` : '',
    '',
    message || 'Расскажите о задаче — что происходит сейчас и какой результат нужен.',
  ]
    .filter(Boolean)
    .join('\n')
  const mailto = `mailto:${CONTACTS.email}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`

  const ready = name.trim().length > 0 && message.trim().length > 0

  return (
    <>
      <PageMeta
        title="Контакты"
        description="Позвоните, напишите в Telegram или оставьте заявку на бесплатную диагностику. Москва, Ленинский проспект, 6с20."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Контакты
          </p>
          <h1 className="site-h1">Давайте поговорим</h1>
          <p className="site-lead">
            Бесплатная диагностика — 45 минут. Сначала разбираемся, как устроены продажи сейчас,
            потом обсуждаем систему. Если CRM вам не нужна, мы скажем это прямо.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="contact-grid">
            <div className="contact-card">
              <p className="site-footer__title">Позвонить</p>
              <p className="site-contact-value">
                <a href={CONTACTS.phoneHref}>{CONTACTS.phone}</a>
              </p>
              <p className="site-contact-value site-contact-value--small">
                <a href={CONTACTS.phone2Href}>{CONTACTS.phone2}</a>
              </p>
              <p className="site-tariff__note">{CONTACTS.hours}</p>
            </div>
            <div className="contact-card">
              <p className="site-footer__title">Написать</p>
              <p className="site-contact-value">
                <a href={`mailto:${CONTACTS.email}`}>{CONTACTS.email}</a>
              </p>
              <p className="site-contact-value site-contact-value--small">
                Telegram: {CONTACTS.telegram}
              </p>
              <p className="site-tariff__note">Отвечаем в рабочее время, обычно в течение часа</p>
            </div>
            <div className="contact-card">
              <p className="site-footer__title">Приехать</p>
              <p className="site-contact-value site-contact-value--small">{CONTACTS.address}</p>
              <p className="site-tariff__note">По предварительной договорённости</p>
            </div>
          </div>
        </div>
      </section>

      <section className="site-section site-section--sunken">
        <div className="site-container">
          <div className="article__body">
            <div>
              <h2 className="site-h2">Заявка на диагностику</h2>
              <p className="article__p">
                Опишите задачу в свободной форме: что за бизнес, сколько менеджеров, какая система
                используется сейчас и что не устраивает. Чем конкретнее — тем полезнее будет разговор.
              </p>

              <form
                className="site-form"
                onSubmit={(event) => {
                  event.preventDefault()
                  window.location.href = mailto
                }}
              >
                <div className="site-form__row">
                  <label className="site-field">
                    <span>Как вас зовут *</span>
                    <input
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="Имя"
                      autoComplete="name"
                      required
                    />
                  </label>
                  <label className="site-field">
                    <span>Компания</span>
                    <input
                      value={company}
                      onChange={(event) => setCompany(event.target.value)}
                      placeholder="Название"
                      autoComplete="organization"
                    />
                  </label>
                </div>

                <label className="site-field">
                  <span>Тема</span>
                  <select value={topic} onChange={(event) => setTopic(event.target.value)}>
                    {TOPICS.map((item) => (
                      <option key={item} value={item}>
                        {item}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="site-field">
                  <span>Задача *</span>
                  <textarea
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                    rows={6}
                    placeholder="Например: два отдела продаж, 12 менеджеров, ведём сделки в таблицах, заявки с сайта теряются"
                    required
                  />
                </label>

                <div className="site-form__foot">
                  {/* A submit button rather than a styled link: a link with no
                      href when the form is incomplete is neither focusable nor
                      a control, and screen readers announce it as text. */}
                  <button type="submit" className="site-btn site-btn--primary" disabled={!ready}>
                    Отправить
                  </button>
                  <p className="site-form__hint">
                    Кнопка открывает почтовую программу с готовым письмом на {CONTACTS.email}.
                    Отправляя заявку, вы соглашаетесь с{' '}
                    <Link to="/privacy">политикой обработки данных</Link>.
                  </p>
                </div>
              </form>
            </div>

            <aside className="article__aside">
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Что будет дальше</p>
                <ul className="site-tariff__features" style={{ marginTop: 12 }}>
                  <li>Отвечаем в течение рабочего дня</li>
                  <li>Согласуем время на 45 минут</li>
                  <li>Проводим разбор процесса продаж</li>
                  <li>Присылаем три идеи автоматизации и смету</li>
                </ul>
              </div>
              <div className="article__toc">
                <p className="article__toc-title">Вопросы</p>
                <ol>
                  {FAQ.slice(0, 3).map((item) => (
                    <li key={item.q}>{item.q}</li>
                  ))}
                  <li>
                    <Link to="/#services">Все услуги</Link>
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
