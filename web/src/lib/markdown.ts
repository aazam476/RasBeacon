/**
 * A small, safe part of Markdown, for plugins' README files: paragraphs, headings, lists,
 * code, **bold**, *italic*, `code` and [links](https://…). It only builds a tree of plain
 * values; `ui/Markdown.tsx` turns that into React elements, so nothing in a README is ever
 * treated as HTML. Anything it doesn't know stays as text.
 */

/** A piece of a line: text, or text with a style. */
export type Inline =
  | { kind: 'text'; text: string }
  | { kind: 'strong'; children: Inline[] }
  | { kind: 'em'; children: Inline[] }
  | { kind: 'code'; text: string }
  | { kind: 'link'; href: string; children: Inline[] };

/** A block: a paragraph, a heading, a list (each item a line), or a block of code. */
export type Block =
  | { kind: 'paragraph'; content: Inline[] }
  | { kind: 'heading'; level: 1 | 2 | 3; content: Inline[] }
  | { kind: 'list'; ordered: boolean; items: Inline[][] }
  | { kind: 'code'; text: string };

// Closing #s ("## Title ##") are dropped only after a space, so "## Using C#" keeps its "#".
const HEADING = /^(#{1,6})\s+(.*?)(?:\s+#+)?\s*$/;
const LIST_ITEM = /^\s{0,3}([-*+]|\d{1,9}[.)])\s+(.*)$/;

/** Reads Markdown text into blocks. */
export function parseMarkdown(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  const blocks: Block[] = [];
  // The paragraph or list being read: lines are added to it until a blank line.
  let paragraph: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;

  const finish = () => {
    if (paragraph.length > 0) {
      blocks.push({ kind: 'paragraph', content: parseInline(paragraph.join(' ')) });
      paragraph = [];
    }
    if (list) {
      blocks.push({ kind: 'list', ordered: list.ordered, items: list.items.map(parseInline) });
      list = null;
    }
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    if (trimmed.startsWith('```')) {
      // A block of code, up to the closing ``` (or the end).
      finish();
      const code: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith('```'); i++) code.push(lines[i]);
      blocks.push({ kind: 'code', text: code.join('\n') });
      continue;
    }
    if (trimmed === '') {
      finish();
      continue;
    }
    const heading = HEADING.exec(trimmed);
    if (heading) {
      finish();
      const level = Math.min(heading[1].length, 3) as 1 | 2 | 3;
      blocks.push({ kind: 'heading', level, content: parseInline(heading[2]) });
      continue;
    }
    const item = LIST_ITEM.exec(line);
    if (item) {
      const ordered = /\d/.test(item[1]);
      if (paragraph.length > 0 || (list && list.ordered !== ordered)) finish();
      list ??= { ordered, items: [] };
      list.items.push(item[2].trim());
      continue;
    }
    if (list) {
      // A line under a list item goes on with that item (Markdown files are often wrapped).
      list.items[list.items.length - 1] += ' ' + trimmed;
      continue;
    }
    // Lines of one paragraph join into one: a README's line breaks are only wrapping.
    paragraph.push(trimmed);
  }
  finish();
  return blocks;
}

/** Characters a backslash can make plain ("\*" is a star, not italic). */
const ESCAPABLE = /[\\`*_[\]()#+\-.!<>{}]/;

/**
 * Remembers a search for "the first match at or after a position". The line is read from left
 * to right, so a later search starts further on: if the last one found nothing, or found a match
 * still ahead, that is the answer again. Without this, a line full of marks that are never
 * closed (a README with thousands of "[") would be searched to its end once per mark.
 */
function rememberedSearch(find: (from: number) => number): (from: number) => number {
  let searchedFrom = Infinity;
  let found = -1;
  return (from) => {
    if (from < searchedFrom || (found !== -1 && found < from)) {
      searchedFrom = from;
      found = find(from);
    }
    return found;
  };
}

/** Reads the styles in one line of text. */
export function parseInline(text: string): Inline[] {
  const out: Inline[] = [];
  let plain = '';
  const flush = () => {
    if (plain) out.push({ kind: 'text', text: plain });
    plain = '';
  };
  // Where the closing marks are, searched once for the whole line (see rememberedSearch).
  const next = (mark: string) => rememberedSearch((from) => text.indexOf(mark, from));
  const nextBacktick = next('`');
  const nextDouble = { '*': next('**'), _: next('__') };
  const nextLinkMiddle = next('](');
  const nextParen = next(')');
  const nextEmEnd = {
    '*': rememberedSearch((from) => closingEm(text, '*', from)),
    _: rememberedSearch((from) => closingEm(text, '_', from)),
  };

  let i = 0;
  while (i < text.length) {
    const c = text[i];

    if (c === '\\' && ESCAPABLE.test(text[i + 1] ?? '')) {
      plain += text[i + 1];
      i += 2;
      continue;
    }

    if (c === '`') {
      const end = nextBacktick(i + 1);
      if (end > i + 1) {
        flush();
        out.push({ kind: 'code', text: text.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }

    // **bold** or __bold__
    if ((c === '*' || c === '_') && text[i + 1] === c) {
      const end = nextDouble[c](i + 2);
      if (end > i + 2) {
        flush();
        out.push({ kind: 'strong', children: parseInline(text.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    }

    // *italic* or _italic_. An underscore only starts one at the start of a word, so names
    // like snake_case stay as they are.
    const opensEm = c === '*' || (c === '_' && !/\w/.test(text[i - 1] ?? ''));
    if (opensEm && text[i + 1] !== undefined && text[i + 1] !== ' ' && text[i + 1] !== c) {
      const end = nextEmEnd[c](i + 1);
      if (end > 0) {
        flush();
        out.push({ kind: 'em', children: parseInline(text.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }

    // [text](https://address): only web addresses become links.
    if (c === '[') {
      const middle = nextLinkMiddle(i + 1);
      const end = middle > 0 ? nextParen(middle + 2) : -1;
      if (middle > i && end > middle) {
        const href = text.slice(middle + 2, end).trim();
        const children = parseInline(text.slice(i + 1, middle));
        flush();
        if (/^https?:\/\/\S+$/i.test(href)) {
          out.push({ kind: 'link', href, children });
        } else {
          out.push(...children);
        }
        i = end + 1;
        continue;
      }
    }

    plain += c;
    i++;
  }
  flush();
  return out;
}

/** Where an italic opened at `from` ends: the next lone `mark` after a non-space (-1: none). */
function closingEm(text: string, mark: string, from: number): number {
  for (let j = from + 1; j < text.length; j++) {
    if (text[j] !== mark || text[j - 1] === ' ' || text[j + 1] === mark) continue;
    // An underscore only ends one at the end of a word.
    if (mark === '_' && /\w/.test(text[j + 1] ?? '')) continue;
    return j;
  }
  return -1;
}
