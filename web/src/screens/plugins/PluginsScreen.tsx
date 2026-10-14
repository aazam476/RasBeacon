import { useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { useDevice } from '@/data/device';
import type { PluginPreview, PluginsState } from '@/data/types';
import { Loaded } from '@/shell/Loaded';
import { ButtonRow, FormError, Icon, LinkRow, Section } from '@/ui';
import { InstallSheet } from './InstallSheet';
import { STATE_LABELS, errorText } from './pluginDisplay';

/**
 * Plugins: the installed ones (each opens its page), and where to get more: RasBeacon's catalog
 * or a .rba file.
 */
export function PluginsScreen() {
  const { plugins, reload } = useDevice();

  return (
    <Loaded data={plugins} what="the plugins" onRetry={() => reload('plugins')}>
      {(state) => <PluginList state={state} />}
    </Loaded>
  );
}

function PluginList({ state }: { state: PluginsState }) {
  const sources = state.sources.length;

  return (
    <>
      {state.plugins.length === 0 ? (
        <NoPlugins />
      ) : (
        <Section title="Installed">
          {state.plugins.map((plugin) => {
            const label = STATE_LABELS[plugin.state];
            return (
              <LinkRow
                key={plugin.id}
                to={`/plugins/${plugin.id}`}
                leading={{ image: plugin.icon }}
                title={plugin.name}
                subtitle={
                  <StateText tone={label.tone}>
                    {plugin.updateVersion ? `${label.text} · Update available` : label.text}
                  </StateText>
                }
              />
            );
          })}
        </Section>
      )}

      <Section
        title="Get plugins"
        footer="Plugins run on their own, each with the memory and storage it is given. They reach the internet only through the VPN when it is on, and can't change RasBeacon's settings."
      >
        <LinkRow
          to="/plugins/catalog"
          leading={{ tile: 'storefront', color: 'blue' }}
          title="Browse plugins"
          value={sources === 1 ? '1 source' : `${sources} sources`}
        />
        <InstallFromFile />
      </Section>
    </>
  );
}

/** A plugin's state in its color. */
function StateText({ tone, children }: { tone: string; children: ReactNode }) {
  const color = tone === 'green' ? 'text-green' : tone === 'red' ? 'text-red' : tone === 'orange' ? 'text-orange' : '';
  return <span className={color}>{children}</span>;
}

function NoPlugins() {
  return (
    <div className="flex flex-col items-center gap-2 px-6 pt-4 pb-2 text-center">
      <Icon name="extension" size={44} className="text-label-3" />
      <p className="text-title3 text-label">No plugins yet</p>
      <p className="max-w-80 text-subhead text-pretty text-label-2">
        Plugins add features like ad blocking or sharing files, and can only do what you allow.
      </p>
    </div>
  );
}

/** "Install from a file": choose a .rba, check it, then the install sheet. */
export function InstallFromFile({ label = 'Install from a file' }: { label?: string }) {
  const { previewPluginFile } = useDevice();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<PluginPreview | null>(null);
  const navigate = useNavigate();

  async function choose(file: File | undefined) {
    if (!file) return;
    setError(null);
    setBusy(true);
    try {
      setPreview(await previewPluginFile(file));
    } catch (problem) {
      setError(errorText(problem));
    } finally {
      setBusy(false);
      if (input.current) input.current.value = '';
    }
  }

  return (
    <>
      <input
        ref={input}
        type="file"
        accept=".rba"
        hidden
        aria-label={label}
        onChange={(event) => void choose(event.target.files?.[0])}
      />
      <ButtonRow
        leading={{ tile: 'upload', color: 'gray' }}
        busy={busy}
        busyLabel="Checking the file…"
        onClick={() => input.current?.click()}
      >
        {label}
      </ButtonRow>
      {error && (
        <div className="row">
          <FormError>{error}</FormError>
        </div>
      )}
      {preview && (
        <InstallSheet
          preview={preview}
          onDone={(id) => {
            setPreview(null);
            if (id) navigate(`/plugins/${id}`);
          }}
        />
      )}
    </>
  );
}
