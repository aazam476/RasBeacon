import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { useDevice } from '@/data/device';
import { errorText } from '@/data/errors';
import type { PluginKept, PluginPreview, PluginsState } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { Loaded } from '@/shell/Loaded';
import { AlertDialog, ButtonRow, FormError, Icon, LinkRow, Section } from '@/ui';
import { InstallSheet } from './InstallSheet';
import { STATE_LABELS, toneText } from './pluginDisplay';

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
                  <>
                    <span className={toneText(label.tone)}>{label.text}</span>
                    {plugin.updateVersion && <span className="text-tint"> · Update available</span>}
                  </>
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

      {state.kept.length > 0 && <KeptData kept={state.kept} />}
    </>
  );
}

/** Data kept from removed plugins, waiting for them to be installed again; each can be deleted. */
function KeptData({ kept }: { kept: PluginKept[] }) {
  const { forgetKeptPluginData } = useDevice();
  const [deleting, setDeleting] = useState<PluginKept | null>(null);
  return (
    <Section
      title="Kept data"
      footer="Kept when you removed these plugins, for when you install them again. Deleting it frees the space."
    >
      {kept.map((item) => (
        <ButtonRow key={item.id} tone="danger" value={formatBytes(item.bytes)} onClick={() => setDeleting(item)}>
          Delete the data of {item.name}
        </ButtonRow>
      ))}
      {deleting && (
        <AlertDialog
          title="Delete the kept data?"
          tone="danger"
          confirmLabel="Delete"
          busyLabel="Deleting…"
          onConfirm={() => forgetKeptPluginData(deleting.id)}
          onClose={() => setDeleting(null)}
        >
          <p>
            The data of <span className="wrap-anywhere">{deleting.name}</span> is deleted for good. Installing it again
            starts with none.
          </p>
        </AlertDialog>
      )}
    </Section>
  );
}

function NoPlugins() {
  return (
    <div className="flex flex-col items-center gap-2 px-6 pt-4 pb-2 text-center">
      <Icon name="extension" size={44} className="text-label-3" />
      <p className="text-title3 text-label">No plugins yet</p>
      <p className="max-w-80 text-subhead text-pretty text-label-2">
        Plugins add features such as ad blocking or file sharing, and can only do what you allow.
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
