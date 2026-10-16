import type { PluginSummary } from '@/data/types';
import { LinkRow, Section } from '@/ui';
import { STATE_LABELS, toneText } from '../plugins/pluginDisplay';

/** The installed plugins, each with its state; nothing when none is installed. */
export function PluginsSection({ plugins }: { plugins: PluginSummary[] }) {
  if (plugins.length === 0) return null;
  return (
    <Section title="Plugins">
      {plugins.map((plugin) => {
        const label = STATE_LABELS[plugin.state];
        return (
          <LinkRow
            key={plugin.id}
            to={`/plugins/${plugin.id}`}
            leading={{ image: plugin.icon }}
            title={plugin.name}
            value={<span className={toneText(label.tone)}>{label.text}</span>}
          />
        );
      })}
    </Section>
  );
}
