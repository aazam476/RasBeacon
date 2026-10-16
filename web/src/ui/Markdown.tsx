import { useMemo, type ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { parseMarkdown, type Block, type Inline } from '@/lib/markdown';

/**
 * Text written in Markdown (a plugin's README), drawn in the app's own type styles. Only plain
 * React elements are made from it, never HTML, so a README can't add anything to the page but
 * text, styles and web links (which open in a new tab).
 */
export function Markdown({ source, className }: { source: string; className?: string }) {
  const blocks = useMemo(() => parseMarkdown(source), [source]);
  return (
    <div className={cn('selectable flex flex-col gap-3 text-subhead wrap-break-word text-label', className)}>
      {blocks.map((block, index) => (
        <BlockView key={index} block={block} />
      ))}
    </div>
  );
}

function BlockView({ block }: { block: Block }) {
  switch (block.kind) {
    case 'heading':
      // Headings inside the README sit under the section's own heading (an h2).
      return block.level === 3 ? (
        <h4 className="text-subhead font-semibold">{inlines(block.content)}</h4>
      ) : (
        <h3 className="text-headline">{inlines(block.content)}</h3>
      );
    case 'list': {
      const List = block.ordered ? 'ol' : 'ul';
      return (
        <List className={cn('flex flex-col gap-1.5 pl-5', block.ordered ? 'list-decimal' : 'list-disc')}>
          {block.items.map((item, index) => (
            <li key={index} className="pl-0.5 text-pretty">
              {inlines(item)}
            </li>
          ))}
        </List>
      );
    }
    case 'code':
      return (
        <pre className="overflow-x-auto overscroll-x-contain rounded-lg bg-fill-2 p-3 font-mono text-footnote">
          {block.text}
        </pre>
      );
    default:
      return <p className="text-pretty">{inlines(block.content)}</p>;
  }
}

/** The pieces of a line as elements. */
function inlines(content: Inline[]): ReactNode[] {
  return content.map((piece, index) => {
    switch (piece.kind) {
      case 'strong':
        return <strong key={index}>{inlines(piece.children)}</strong>;
      case 'em':
        return <em key={index}>{inlines(piece.children)}</em>;
      case 'code':
        return (
          <code key={index} className="rounded-[5px] bg-fill-2 px-1 py-px font-mono text-[0.92em]">
            {piece.text}
          </code>
        );
      case 'link':
        return (
          <a
            key={index}
            href={piece.href}
            target="_blank"
            rel="noreferrer"
            className="text-tint underline decoration-tint/40 underline-offset-2"
          >
            {inlines(piece.children)}
          </a>
        );
      default:
        return piece.text;
    }
  });
}
