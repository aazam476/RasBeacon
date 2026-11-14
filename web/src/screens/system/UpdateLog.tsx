import { useEffect, useRef, useState } from 'react';
import type { UpdateLogLine } from '@/data/types';
import { Disclosure } from '@/ui';

/** Characters in a progress bar: each stands for 5%. */
const BAR_WIDTH = 20;

/**
 * How often a line is added while RasBeacon restarts and can't add its own: the same pace rbd
 * keeps while it installs (updateLogEvery in internal/agent/updatelog.go).
 */
const RESTART_LINE_EVERY_MS = 5000;

/** After this long, a restart is taking longer than usual (it normally takes about 2 minutes). */
const RESTART_SLOW_SECONDS = 180;

/** A progress line as a terminal shows it: "45% [XXXXXXXXX...........]". */
function progressBar(percent: number): string {
  const filled = Math.round((Math.min(100, Math.max(0, percent)) / 100) * BAR_WIDTH);
  return `${percent}% [${'X'.repeat(filled)}${'.'.repeat(BAR_WIDTH - filled)}]`;
}

/** "45 s", "2 min 5 s". */
function formatSeconds(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  return minutes > 0 ? `${minutes} min ${seconds % 60} s` : `${seconds} s`;
}

/** The line the page adds `seconds` into a restart, while RasBeacon can't be reached. */
function restartLine(seconds: number): string {
  const line = `Still restarting (${formatSeconds(seconds)} so far)`;
  if (seconds < RESTART_SLOW_SECONDS) return line;
  return `${line}. This is taking longer than usual; if the new version doesn't start, RasBeacon goes back to this one by itself.`;
}

/**
 * Counts the seconds since `active` became true, in steps of RESTART_LINE_EVERY_MS: one more line
 * for each step. 0 while not active.
 */
function useRestartSteps(active: boolean): number {
  const [steps, setSteps] = useState(0);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setSteps((count) => count + 1), RESTART_LINE_EVERY_MS);
    return () => {
      clearInterval(timer);
      setSteps(0);
    };
  }, [active]);
  return steps;
}

/**
 * What RasBeacon is doing while it installs an update, step by step, so a long install doesn't
 * look stuck: the signature check, the file arriving and being written (a progress bar that
 * moves on in place, with the megabytes), the final check, and the restart, or why it failed.
 * rbd adds a line at least every 5 seconds (internal/agent/updatelog.go); while it restarts
 * (`restarting`) and can't, the page adds one every 5 seconds itself. Closed by default; a row
 * of the Software section.
 */
export function UpdateLog({ lines, restarting = false }: { lines: UpdateLogLine[]; restarting?: boolean }) {
  const box = useRef<HTMLPreElement>(null);
  const restartSteps = useRestartSteps(restarting);
  const shown: UpdateLogLine[] = [...lines];
  for (let step = 1; step <= restartSteps; step++) {
    shown.push({ text: restartLine((step * RESTART_LINE_EVERY_MS) / 1000) });
  }

  // Keep the newest line in view, like a terminal, unless the reader scrolled up to look back.
  const count = shown.length;
  const last = shown[count - 1];
  useEffect(() => {
    const element = box.current;
    if (!element) return;
    const nearBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 48;
    if (nearBottom) element.scrollTop = element.scrollHeight;
  }, [count, last?.percent, last?.detail]);

  return (
    <Disclosure title="Update logs" leading={{ icon: 'terminal' }}>
      <pre
        ref={box}
        aria-label="Update log"
        className="max-h-64 overflow-auto rounded-lg bg-fill p-3 font-mono text-footnote leading-relaxed whitespace-pre-wrap text-label"
      >
        {shown.map((line, index) => (
          <div key={index}>
            {line.text}
            {line.percent !== undefined && ` ${progressBar(line.percent)}`}
            {line.detail && ` ${line.detail}`}
          </div>
        ))}
      </pre>
    </Disclosure>
  );
}
