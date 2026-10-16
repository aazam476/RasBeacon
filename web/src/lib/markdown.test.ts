import { describe, expect, it } from 'vitest';
import { parseInline, parseMarkdown } from './markdown';

describe('parseMarkdown', () => {
  it('joins wrapped lines and splits paragraphs at blank lines', () => {
    const blocks = parseMarkdown('AdGuard Home answers your\ndevices.\n\nSecond paragraph.\n');
    expect(blocks).toEqual([
      { kind: 'paragraph', content: [{ kind: 'text', text: 'AdGuard Home answers your devices.' }] },
      { kind: 'paragraph', content: [{ kind: 'text', text: 'Second paragraph.' }] },
    ]);
  });

  it('reads lists, with wrapped items, and headings', () => {
    const blocks = parseMarkdown(
      '# Using it\n\n- **Fan:** three steps. From\n  *Fan on at* on.\n- Power button.\n1. One',
    );
    expect(blocks[0]).toEqual({ kind: 'heading', level: 1, content: [{ kind: 'text', text: 'Using it' }] });
    expect(blocks[1]).toEqual({
      kind: 'list',
      ordered: false,
      items: [
        [
          { kind: 'strong', children: [{ kind: 'text', text: 'Fan:' }] },
          { kind: 'text', text: ' three steps. From ' },
          { kind: 'em', children: [{ kind: 'text', text: 'Fan on at' }] },
          { kind: 'text', text: ' on.' },
        ],
        [{ kind: 'text', text: 'Power button.' }],
      ],
    });
    expect(blocks[2]).toEqual({ kind: 'list', ordered: true, items: [[{ kind: 'text', text: 'One' }]] });
  });

  it('keeps code as it is', () => {
    expect(parseMarkdown('```\na **b**\n```')).toEqual([{ kind: 'code', text: 'a **b**' }]);
    expect(parseInline('File Explorer, `\\\\<address>`')).toEqual([
      { kind: 'text', text: 'File Explorer, ' },
      { kind: 'code', text: '\\\\<address>' },
    ]);
  });
});

describe('parseInline', () => {
  it('makes links only of web addresses', () => {
    expect(parseInline('[the template](https://github.com/x/y)')).toEqual([
      { kind: 'link', href: 'https://github.com/x/y', children: [{ kind: 'text', text: 'the template' }] },
    ]);
    expect(parseInline('[click](javascript:alert(1))')).toEqual([
      { kind: 'text', text: 'click' },
      { kind: 'text', text: ')' },
    ]);
  });

  it('leaves what it does not know as text', () => {
    expect(parseInline('<b>hi</b> & snake_case_name, 2 * 3 * 4')).toEqual([
      { kind: 'text', text: '<b>hi</b> & snake_case_name, 2 * 3 * 4' },
    ]);
    expect(parseInline('\\*not italic\\*')).toEqual([{ kind: 'text', text: '*not italic*' }]);
  });
});
