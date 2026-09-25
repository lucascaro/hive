// Markdown as React elements, for plans an agent wrote (#457).
//
// `marked` is used for its lexer ONLY. Its HTML renderer, and every
// other path that ends in innerHTML, is deliberately not: the text is
// model output, and a plan with a <script> or an onerror in it must
// read as that text, not run. So raw HTML tokens render as literal text,
// links render as text plus their address and never navigate, and an
// unknown token falls back to its source. Same stance as WhatsNew.tsx.

import { lexer, type Token, type Tokens } from 'marked';
import { type ReactNode, useMemo } from 'react';

export function Markdown({ source }: { source: string }): ReactNode {
  const tokens = useMemo(() => {
    try {
      return lexer(source);
    } catch {
      // A lexer failure costs the formatting, never the plan.
      return null;
    }
  }, [source]);
  if (!tokens) return <pre className="hv-md-code">{source}</pre>;
  return <>{blocks(tokens)}</>;
}

function blocks(tokens: Token[]): ReactNode[] {
  return tokens.map((t, i) => block(t, i));
}

function block(t: Token, key: number): ReactNode {
  switch (t.type) {
    case 'space':
      return null;
    case 'heading': {
      const h = t as Tokens.Heading;
      // Plans use # freely; h1 inside a modal would outrank its title.
      const level = Math.min(6, h.depth + 2);
      const Tag = `h${level}` as 'h3' | 'h4' | 'h5' | 'h6';
      return (
        <Tag key={key} className="hv-md-heading">
          {inline(h.tokens)}
        </Tag>
      );
    }
    case 'paragraph':
      return <p key={key}>{inline((t as Tokens.Paragraph).tokens)}</p>;
    case 'text': {
      const tx = t as Tokens.Text;
      return <p key={key}>{tx.tokens ? inline(tx.tokens) : tx.text}</p>;
    }
    case 'code': {
      const c = t as Tokens.Code;
      return (
        <pre key={key} className="hv-md-code">
          <code>{c.text}</code>
        </pre>
      );
    }
    case 'blockquote':
      return (
        <blockquote key={key}>
          {blocks((t as Tokens.Blockquote).tokens)}
        </blockquote>
      );
    case 'hr':
      return <hr key={key} />;
    case 'list': {
      const l = t as Tokens.List;
      const items = l.items.map((it, j) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: static render of a parsed list
        <li key={j}>
          {it.task ? (
            <input type="checkbox" checked={!!it.checked} readOnly disabled />
          ) : null}
          {listItem(it)}
        </li>
      ));
      return l.ordered ? (
        <ol key={key} start={typeof l.start === 'number' ? l.start : undefined}>
          {items}
        </ol>
      ) : (
        <ul key={key}>{items}</ul>
      );
    }
    case 'table': {
      const tb = t as Tokens.Table;
      return (
        <table key={key} className="hv-md-table">
          <thead>
            <tr>
              {tb.header.map((c, j) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: static render of a parsed table
                <th key={j}>{inline(c.tokens)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {tb.rows.map((row, r) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: static render of a parsed table
              <tr key={r}>
                {row.map((c, j) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: static render of a parsed table
                  <td key={j}>{inline(c.tokens)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      );
    }
    default:
      // html, def, and anything a future marked adds: the source, as text.
      return (
        <p key={key} className="hv-md-raw">
          {'raw' in t ? String(t.raw) : ''}
        </p>
      );
  }
}

// A tight list item's children are bare `text` tokens; rendering them
// as paragraphs would space the list out.
function listItem(it: Tokens.ListItem): ReactNode {
  return it.tokens.map((t, i) =>
    t.type === 'text' ? (
      // biome-ignore lint/suspicious/noArrayIndexKey: static render
      <span key={i}>
        {(t as Tokens.Text).tokens
          ? inline((t as Tokens.Text).tokens ?? [])
          : (t as Tokens.Text).text}
      </span>
    ) : (
      block(t, i)
    ),
  );
}

function inline(tokens: Token[] | undefined): ReactNode[] {
  return (tokens ?? []).map((t, i) => inlineToken(t, i));
}

function inlineToken(t: Token, key: number): ReactNode {
  switch (t.type) {
    case 'text':
    case 'escape': {
      const tx = t as Tokens.Text;
      return tx.tokens ? (
        <span key={key}>{inline(tx.tokens)}</span>
      ) : (
        <span key={key}>{tx.text}</span>
      );
    }
    case 'strong':
      return <strong key={key}>{inline((t as Tokens.Strong).tokens)}</strong>;
    case 'em':
      return <em key={key}>{inline((t as Tokens.Em).tokens)}</em>;
    case 'del':
      return <del key={key}>{inline((t as Tokens.Del).tokens)}</del>;
    case 'codespan':
      return <code key={key}>{(t as Tokens.Codespan).text}</code>;
    case 'br':
      return <br key={key} />;
    case 'link': {
      // Shown, never followed: a click in a review is for selecting
      // text to comment on, and the address is model output.
      const l = t as Tokens.Link;
      return (
        <span key={key} className="hv-md-link" title={l.href}>
          {inline(l.tokens)}
        </span>
      );
    }
    case 'image': {
      const im = t as Tokens.Image;
      return (
        <span key={key} className="hv-md-link" title={im.href}>
          [{im.text}]
        </span>
      );
    }
    default:
      // Inline html and anything unknown: literal source text.
      return <span key={key}>{'raw' in t ? String(t.raw) : ''}</span>;
  }
}
