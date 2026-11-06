/**
 * The privacy and cookie policy page.
 *
 * Written to answer the three questions the consent banner promises to answer:
 * what is stored, why, and how to withdraw. It is also the destination of the
 * banner's "policy" link and of `#cookies` deep links.
 *
 * The text describes what this build actually does. If analytics is added later,
 * this page has to change in the same pull request — a policy that describes a
 * different build is worse than no policy.
 */
import { Link } from 'react-router-dom'
import { CONTACTS } from '../content'
import { openCookieSettings } from '../components/CookieConsent'
import { PageMeta } from './components/PageMeta'

const SECTIONS = [
  {
    id: 'what',
    title: 'Какие данные мы собираем',
    body: [
      'Через форму на странице контактов вы сообщаете нам имя, название компании и описание задачи. Эти данные вы отправляете сами — письмом с вашей почтовой программы. Мы используем их только для ответа на обращение.',
      'Технические данные: сервер записывает IP-адрес, время запроса и адрес страницы в журнале доступа. Это нужно для защиты от атак и разбора сбоев, журналы хранятся 30 дней.',
      'Если вы вошли в личную CRM — мы обрабатываем данные вашей учётной записи в объёме, необходимом для работы сервиса, согласно договору с вашей компанией.',
    ],
  },
  {
    id: 'cookies',
    title: 'Файлы cookie',
    body: [
      'Cookie — небольшой файл, который сайт сохраняет в вашем браузере. Мы используем три категории, и две из них включаются только с вашего согласия.',
    ],
  },
  {
    id: 'necessary',
    title: 'Необходимые cookie',
    body: [
      'Работают всегда, потому что без них сайт не работает: сессия входа в CRM, защита форм от автоматических отправок, запоминание выбранного оформления. Согласие для них не требуется, отключить их нельзя.',
    ],
  },
  {
    id: 'analytics',
    title: 'Аналитические cookie',
    body: [
      'Помогают понять, какие страницы читают, а какие закрывают сразу. Сейчас в этой сборке аналитика не подключена: категория существует, выбор сохраняется, но сторонние счётчики не загружаются. Если мы их добавим, политика будет обновлена до запуска, а ваше сохранённое решение — переспрошено.',
    ],
  },
  {
    id: 'marketing',
    title: 'Маркетинговые cookie',
    body: [
      'Нужны, чтобы показывать рекламу тем, кто уже интересовался услугами, и не показывать её тем, кто не интересовался. Как и с аналитикой: категория есть, счётчиков в этой сборке нет, выбор сохраняется.',
    ],
  },
  {
    id: 'choice',
    title: 'Как изменить или отозвать согласие',
    body: [
      'Нажмите кнопку ниже — откроется та же панель, что и при первом визите. Там можно выключить аналитику и маркетинг отдельно. Отзыв согласия не влияет на законность обработки, которая происходила до него.',
      'Вы также можете удалить сохранённое решение в настройках браузера: очистить данные сайта или удалить запись forgecrm.cookie-consent. После этого баннер появится снова при следующем визите.',
    ],
  },
  {
    id: 'rights',
    title: 'Ваши права',
    body: [
      'Вы можете запросить сведения об обработке ваших персональных данных, потребовать их уточнения, блокирования или удаления, а также отозвать согласие на обработку. Запрос направляйте на ' + CONTACTS.email + ' с адреса, на который вы получали ответ от нас, — так мы сможем подтвердить, что запрос от вас.',
      'Срок ответа на обращение — 10 рабочих дней. Если данные обрабатываются по договору с вашей компанией, запрос удобнее направить её администратору: он управляет учётными записями.',
    ],
  },
  {
    id: 'contact',
    title: 'Кто отвечает за обработку',
    body: [
      CONTACTS.legal + ', ' + CONTACTS.address + '. Телефон: ' + CONTACTS.phone + '. Почта: ' + CONTACTS.email + '.',
    ],
  },
]

export function PrivacyPage() {
  return (
    <>
      <PageMeta
        title="Политика обработки данных и cookie"
        description="Какие данные собирает сайт, какие cookie используются, как изменить или отозвать согласие."
      />

      <section className="blog-head">
        <div className="site-container">
          <p className="site-eyebrow">
            <span className="site-eyebrow__dot" aria-hidden="true" />
            Правовая информация
          </p>
          <h1 className="site-h1">Политика обработки данных и cookie</h1>
          <p className="site-lead">
            Коротко: необходимые cookie нужны для работы сайта, аналитические и маркетинговые
            включаются только с вашего согласия, а отозвать его можно в один клик.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <div className="article__body">
            <article className="article">
              {SECTIONS.map((section) => (
                <section key={section.id}>
                  <h2 className="site-h2" id={section.id}>
                    {section.title}
                  </h2>
                  {section.body.map((paragraph) => (
                    <p className="article__p" key={paragraph.slice(0, 40)}>
                      {paragraph}
                    </p>
                  ))}
                  {section.id === 'choice' ? (
                    <div className="site-hero__actions" style={{ marginBottom: 12 }}>
                      <button
                        type="button"
                        className="site-btn site-btn--primary"
                        onClick={() => openCookieSettings()}
                      >
                        Открыть настройки cookie
                      </button>
                      <Link className="site-btn site-btn--secondary" to="/contacts">
                        Задать вопрос
                      </Link>
                    </div>
                  ) : null}
                </section>
              ))}

              <p className="article__p" style={{ marginTop: 32, opacity: 0.75 }}>
                Редакция от 1 ноября 2026 года. Предыдущие редакции предоставляются по запросу.
              </p>
            </article>

            <aside className="article__aside">
              <div className="article__toc">
                <p className="article__toc-title">Содержание</p>
                <ol>
                  {SECTIONS.map((section) => (
                    <li key={section.id}>
                      <a href={`#${section.id}`}>{section.title}</a>
                    </li>
                  ))}
                </ol>
              </div>
              <div className="article__author" style={{ display: 'block' }}>
                <p className="site-footer__title">Связанные документы</p>
                <ul className="site-tariff__features" style={{ marginTop: 12 }}>
                  <li>
                    <Link to="/contacts">Обработка обращений</Link>
                  </li>
                  <li>
                    <Link to="/about">О компании</Link>
                  </li>
                </ul>
              </div>
            </aside>
          </div>
        </div>
      </section>
    </>
  )
}
