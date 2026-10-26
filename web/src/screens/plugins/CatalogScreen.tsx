import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { useDevice } from '@/data/device';
import type { PluginCatalog, PluginCatalogEntry, PluginPreview } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { usePageTitle } from '@/shell/navigation';
import { ActivityIndicator, Button, ButtonRow, FormError, IconButton, Notice, Row, Section, TextField } from '@/ui';
import { InstallSheet } from './InstallSheet';
import { errorText } from './pluginDisplay';

/** "8 Oct, 14:05": when the catalog was last checked. */
function checkedLabel(iso: string | undefined): string {
  if (!iso) return 'Not checked yet.';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return `Checked ${date.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}.`;
}

/**
 * The plugins RasBeacon's sources offer: GitHub repositories whose newest release holds .rba
 * files. GitHub is only asked when this page opens for the first time, or with Check now.
 */
export function CatalogScreen() {
  usePageTitle('Browse plugins');
  const { pluginCatalog } = useDevice();
  const [catalog, setCatalog] = useState<PluginCatalog | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);

  const load = useCallback(
    async (refresh: boolean) => {
      setChecking(true);
      setError(null);
      try {
        setCatalog(await pluginCatalog(refresh));
      } catch (problem) {
        setError(errorText(problem));
      } finally {
        setChecking(false);
      }
    },
    [pluginCatalog],
  );

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect -- the catalog arrives when the router answers
    void load(false);
  }, [load]);

  const bySource = new Map<string, PluginCatalogEntry[]>();
  for (const entry of catalog?.entries ?? []) {
    bySource.set(entry.source, [...(bySource.get(entry.source) ?? []), entry]);
  }

  return (
    <>
      <Section footer={catalog ? checkedLabel(catalog.checkedAt) : undefined}>
        <ButtonRow
          leading={{ tile: 'sync', color: 'blue' }}
          busy={checking}
          busyLabel="Checking…"
          onClick={() => void load(true)}
        >
          Check now
        </ButtonRow>
      </Section>

      {error && (
        <Notice tone="danger" role="alert">
          <p>{error}</p>
        </Notice>
      )}
      {Object.entries(catalog?.errors ?? {}).map(([source, problem]) => (
        <Notice key={source} tone="warning" title={source}>
          <p className="wrap-break-word">{problem}</p>
        </Notice>
      ))}

      {catalog === null && !error ? (
        <div className="flex justify-center py-8">
          <ActivityIndicator label="Loading the catalog" />
        </div>
      ) : (
        [...bySource.entries()].map(([source, entries]) => (
          <Section key={source} title={<span className="wrap-anywhere">{source}</span>}>
            {entries.map((entry) => (
              <CatalogRow key={entry.file} entry={entry} />
            ))}
          </Section>
        ))
      )}
      {catalog !== null && catalog.entries.length === 0 && !error && (
        <p className="py-4 text-center text-subhead text-label-2">No plugins found in the sources.</p>
      )}

      {/* After a source is added, ask GitHub for what it offers; after one is removed, the
          saved list (now without it) is enough. */}
      <SourcesSection onSaved={(added) => void load(added)} />
    </>
  );
}

/** One plugin with Get, Update or Installed, as in an app store. */
function CatalogRow({ entry }: { entry: PluginCatalogEntry }) {
  const { previewCatalogPlugin } = useDevice();
  const navigate = useNavigate();
  const [preview, setPreview] = useState<PluginPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const installed = entry.installedVersion !== undefined;
  const newer = installed && entry.updateAvailable === true;

  async function get() {
    setBusy(true);
    setError(null);
    try {
      setPreview(await previewCatalogPlugin(entry.source, entry.file));
    } catch (problem) {
      setError(errorText(problem));
    } finally {
      setBusy(false);
    }
  }

  const size = entry.downloadSize ? entry.size + entry.downloadSize : entry.size;
  const details = [entry.version && `Version ${entry.version}`, formatBytes(size)].filter(Boolean).join(' · ');

  return (
    <>
      <Row
        leading={entry.icon ? { image: entry.icon } : { tile: 'extension', color: 'gray' }}
        title={entry.name}
        subtitle={
          <>
            {entry.description && <span className="block text-label">{entry.description}</span>}
            <span className="block">
              {!entry.runsHere
                ? "Doesn't run on this Raspberry Pi"
                : entry.needsRasBeacon
                  ? `Needs RasBeacon ${entry.needsRasBeacon} or newer`
                  : details}
            </span>
          </>
        }
        trailing={
          installed && !newer ? (
            <Button size="sm" variant="secondary" onClick={() => entry.id && navigate(`/plugins/${entry.id}`)}>
              Open
            </Button>
          ) : (
            <Button
              size="sm"
              variant="secondary"
              loading={busy}
              disabled={!entry.runsHere || Boolean(entry.needsRasBeacon)}
              onClick={() => void get()}
              aria-label={`${newer ? 'Update' : 'Get'} ${entry.name}`}
            >
              {newer ? 'Update' : 'Get'}
            </Button>
          )
        }
      />
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

/**
 * Where the catalog looks: RasBeacon's own source, and GitHub repositories the owner adds.
 * `onSaved` runs after the list is saved (`added` is true when a source was added), so the
 * catalog above can be loaded again.
 */
function SourcesSection({ onSaved }: { onSaved: (added: boolean) => void }) {
  const { plugins, savePluginSources } = useDevice();
  const [adding, setAdding] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const sources = plugins.data?.sources ?? [];
  // Every source, the built-in one too: removing it is allowed, and typing its name adds it back.
  const listed = sources.map((s) => s.source);

  async function save(next: string[]) {
    setSaving(true);
    setError(null);
    try {
      await savePluginSources(next);
      setAdding('');
      onSaved(next.length > listed.length);
    } catch (problem) {
      setError(errorText(problem));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Section
      title="Sources"
      footer="A source is a GitHub repository whose newest release holds plugins. Anyone can make one: add only sources you trust."
    >
      {sources.map((source) => (
        <Row
          key={source.source}
          leading={{ icon: 'storefront' }}
          title={<span className="wrap-anywhere">{source.source}</span>}
          value={source.builtIn ? 'Built in' : undefined}
          trailing={
            <IconButton
              icon="delete"
              ariaLabel={`Remove ${source.source}`}
              disabled={saving}
              onClick={() => void save(listed.filter((s) => s !== source.source))}
            />
          }
        />
      ))}
      <form
        className="row-group"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          if (adding.trim()) void save([...listed, adding.trim()]);
        }}
      >
        <TextField
          label="Add a source"
          placeholder="owner/repository"
          autoComplete="off"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={adding}
          onChange={setAdding}
          error={error ?? undefined}
        />
        <div className="row">
          <Button type="submit" variant="secondary" loading={saving} disabled={adding.trim() === ''}>
            Add
          </Button>
        </div>
      </form>
    </Section>
  );
}
