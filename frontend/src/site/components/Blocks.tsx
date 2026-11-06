/**
 * Article blocks.
 *
 * One renderer per block kind, so a new block type is added in one place and
 * every layout gets it. The reading column, its width and its rhythm belong to
 * the stylesheet, which is what lets the media layout widen the text while the
 * documentation layout tightens it without a second renderer.
 */
import { Link } from 'react-router-dom'
import type { Block } from '../content'

export function Blocks({ blocks }: { blocks: readonly Block[] }) {
  return (
    <div className="prose">
      {blocks.map((block, index) => {
        switch (block.kind) {
          case 'h2':
            return (
              <h2 className="prose__h2" id={anchor(block.text)} key={index}>
                {block.text}
              </h2>
            )
          case 'h3':
            return (
              <h3 className="prose__h3" id={anchor(block.text)} key={index}>
                {block.text}
              </h3>
            )
          case 'p':
            return (
              <p className="prose__p" key={index}>
                {block.text}
              </p>
            )
          case 'list':
            return block.ordered ? (
              <ol className="prose__list" key={index}>
                {block.items.map((item, itemIndex) => (
                  <li key={itemIndex}>{item}</li>
                ))}
              </ol>
            ) : (
              <ul className="prose__list" key={index}>
                {block.items.map((item, itemIndex) => (
                  <li key={itemIndex}>{item}</li>
                ))}
              </ul>
            )
          case 'quote':
            return (
              <blockquote className="prose__quote" key={index}>
                <p>{block.text}</p>
              </blockquote>
            )
          case 'note':
            return (
              <aside className="prose__note" key={index}>
                <p className="prose__note-title">{block.title}</p>
                <p>{block.text}</p>
              </aside>
            )
          case 'table':
            return (
              <div className="prose__table-wrap" key={index}>
                <table className="prose__table">
                  <thead>
                    <tr>
                      {block.head.map((cell) => (
                        <th key={cell} scope="col">
                          {cell}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {block.rows.map((row, rowIndex) => (
                      <tr key={rowIndex}>
                        {row.map((cell, cellIndex) => (
                          <td key={cellIndex}>{cell}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          case 'cta':
            return (
              <aside className="prose__cta" key={index}>
                <div>
                  <p className="prose__cta-title">{block.title}</p>
                  <p className="prose__cta-text">{block.text}</p>
                </div>
                <Link className="site-btn site-btn--primary" to={block.href}>
                  {block.label}
                </Link>
              </aside>
            )
          default:
            return null
        }
      })}
    </div>
  )
}

/**
 * A heading anchor. Cyrillic survives `toLowerCase` and the replacement, so a
 * table of contents can link to a heading written in Russian.
 */
export function anchor(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .trim()
    .replace(/\s+/g, '-')
}

/** The headings of an article, for a table of contents. */
export function articleOutline(blocks: readonly Block[]): { id: string; text: string }[] {
  return blocks
    .filter((block): block is Extract<Block, { kind: 'h2' }> => block.kind === 'h2')
    .map((block) => ({ id: anchor(block.text), text: block.text }))
}
