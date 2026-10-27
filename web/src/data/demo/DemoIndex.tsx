import { useDocumentTitle } from '@/lib/useDocumentTitle';
import { LinkRow, Section } from '@/ui';
import { SCENARIOS } from './scenarios';

/** Link that reloads the app with a scenario applied and opens its page. */
function scenarioHref(id: string | null, route: string): string {
  const query = id ? `?scenario=${id}` : '';
  return `${window.location.pathname}${query}#${route}`;
}

/**
 * The demo's table of contents (`/#/demo`): every state of every screen, filled with sample
 * data, to look through every one. Part of the demo only.
 */
export function DemoIndex() {
  useDocumentTitle('Demo states');
  const groups = [...new Set(SCENARIOS.map((scenario) => scenario.group))];

  return (
    <main className="mx-auto flex w-full max-w-[64rem] flex-col gap-7 px-4 pt-[calc(env(safe-area-inset-top)+3rem)] pb-16 lg:px-8 lg:pt-12">
      <header className="flex max-w-[40rem] flex-col gap-2">
        <h1 className="text-large-title text-label">Demo states</h1>
        <p className="text-body text-label-2">
          There is no router behind this demo: it shows sample data. Pick a state to open its page as the router would
          show it. Buttons work on the sample data, and forms reject wrong input as the router would.
        </p>
      </header>

      <Section>
        <LinkRow
          reload
          to={scenarioHref(null, '/')}
          leading={{ icon: 'restart_alt' }}
          title="Start over with everything working"
        />
      </Section>

      <div className="flex flex-col gap-7 lg:block lg:columns-2 lg:gap-7 lg:*:mb-7 lg:*:break-inside-avoid">
        {groups.map((group) => (
          <Section key={group} title={group}>
            {SCENARIOS.filter((scenario) => scenario.group === group).map((scenario) => (
              <LinkRow key={scenario.id} reload to={scenarioHref(scenario.id, scenario.route)} title={scenario.title} />
            ))}
          </Section>
        ))}
      </div>
    </main>
  );
}
