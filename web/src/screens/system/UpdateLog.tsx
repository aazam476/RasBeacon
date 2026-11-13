import { useEffect, useRef } from 'react';
import type { UpdateLogLine } from '@/data/types';
import { Disclosure } from '@/ui';

/** Characters in a progress bar: each stands for 5%. */
const BAR_WIDTH = 20;

/** A progress line as a terminal shows it: "45% [XXXXXXXXX...........]". */
function progressBar(percent: number): string {
  const filled = Math.round((Math.min(100, Math.max(0, percent)) / 100) * BAR_WIDTH);
  return `${percent}% [${'X'.repeat(filled)}${'.'.repeat(BAR_WIDTH - filled)}]`;
}

/**
 * What RasBeacon is doing while it installs an update, step by step, so a long install doesn't
 * look stuck: the signature check, the file arriving and being written (a progress bar that
 * moves on in place), the final check, and the restart, or why it failed. Closed by default; a
 * row of the Software section. The lines come from rbd (internal/agent/updatelog.go).
 */
export function UpdateLog({ lines }: { lines: UpdateLogLine[] }) {
  const box = useRef<HTMLPreElement>(null);

  // Keep the newest line in view, like a terminal, unless the reader scrolled up to look back.
  const count = lines.length;
  const last = lines[count - 1];
  useEffect(() => {
    const element = box.current;
    if (!element) return;
    const nearBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 48;
    if (nearBottom) element.scrollTop = element.scrollHeight;
  }, [count, last?.percent]);

  return (
    <Disclosure title="Update logs" leading={{ icon: 'terminal' }}>
      <pre
        ref={box}
        aria-label="Update log"
        className="max-h-64 overflow-auto rounded-lg bg-fill p-3 font-mono text-footnote leading-relaxed whitespace-pre-wrap text-label"
      >
        {lines.map((line, index) => (
          <div key={index}>
            {line.text}
            {line.percent !== undefined && ` ${progressBar(line.percent)}`}
          </div>
        ))}
      </pre>
    </Disclosure>
  );
}
