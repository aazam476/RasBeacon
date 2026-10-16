import { useState, type FormEvent } from 'react';
import { Navigate, useNavigate, useParams } from 'react-router';
import { useDevice } from '@/data/device';
import type { Plugin, PluginPreview } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { usePageTitle } from '@/shell/navigation';
import {
  AlertDialog,
  Badge,
  BreakableText,
  ButtonRow,
  Disclosure,
  FormActions,
  FormSwitch,
  Icon,
  LinkRow,
  Markdown,
  Notice,
  Row,
  Section,
  StatRow,
  SwitchRow,
} from '@/ui';
import { InstallSheet } from './InstallSheet';
import { PluginFields } from './PluginFields';
import { InstallFromFile } from './PluginsScreen';
import {
  byline,
  choicesOf,
  draftOf,
  isBusy,
  PERMISSION_ICONS,
  sourceLabel,
  splitMetric,
  STATE_LABELS,
  errorText,
} from './pluginDisplay';

/** One plugin: how it is doing, opening it, its settings, log, updates and removal. */
export function PluginScreen() {
  const { id } = useParams();
  const { plugins, reload } = useDevice();
  const plugin = plugins.data?.plugins.find((p) => p.id === id);
  usePageTitle(plugin?.name ?? 'Plugin');

  return (
    <Loaded data={plugins} what="the plugin" onRetry={() => reload('plugins')}>
      {() => (plugin ? <PluginPage key={plugin.id} plugin={plugin} /> : <Navigate to="/plugins" replace />)}
    </Loaded>
  );
}

function PluginPage({ plugin }: { plugin: Plugin }) {
  if (plugin.broken) return <BrokenPluginPage plugin={plugin} />;
  return <WorkingPluginPage plugin={plugin} />;
}

/**
 * A plugin whose files can't be read: nothing is known of it but its ID, so only what can
 * help is offered: its log, installing it again from a file, and removing it.
 */
function BrokenPluginPage({ plugin }: { plugin: Plugin }) {
  return (
    <>
      <Hero plugin={plugin} />
      <LogSection plugin={plugin} />
      <AboutSection plugin={plugin} />
      <UpdateSection plugin={plugin} />
      <RemoveSection plugin={plugin} />
    </>
  );
}

function WorkingPluginPage({ plugin }: { plugin: Plugin }) {
  const busy = isBusy(plugin.state);
  // What to do next only applies once it is installed and works (not while it installs, or
  // when installing failed).
  const showNextSteps = plugin.afterInstall && !busy && plugin.state !== 'failed';

  return (
    <>
      <Hero plugin={plugin} />

      {plugin.metrics.length > 0 && (
        <Section>
          <StatRow stats={plugin.metrics.map((metric) => ({ label: metric.label, ...splitMetric(metric.value) }))} />
        </Section>
      )}

      <RunningSection plugin={plugin} />

      {showNextSteps && (
        <Notice tone="info" icon="info" title="Next steps">
          <p className="wrap-break-word">{plugin.afterInstall}</p>
        </Notice>
      )}

      {!busy && <SettingsForm plugin={plugin} />}

      <PermissionsSection plugin={plugin} />
      <LogSection plugin={plugin} />
      <AboutSection plugin={plugin} />
      {plugin.readme && (
        <Section title="About this plugin">
          <div className="row py-3">
            <Markdown source={plugin.readme} />
          </div>
        </Section>
      )}
      {!busy && <UpdateSection plugin={plugin} />}
      {!busy && <RemoveSection plugin={plugin} />}
    </>
  );
}

/** The icon, name, state and the Open buttons. */
function Hero({ plugin }: { plugin: Plugin }) {
  const label = STATE_LABELS[plugin.state];
  const openable = plugin.ports.filter((port) => port.url);

  return (
    <div className="flex items-start gap-4">
      {plugin.icon ? (
        <img
          src={plugin.icon}
          alt=""
          draggable={false}
          className="size-20 shrink-0 rounded-[18px] shadow-sm lg:size-16 lg:rounded-[14px]"
        />
      ) : (
        // Its icon can't be read (its files are damaged): a plain one instead.
        <span
          aria-hidden="true"
          className="grid size-20 shrink-0 place-items-center rounded-[18px] bg-tile-gray text-white shadow-sm lg:size-16 lg:rounded-[14px]"
        >
          <Icon name="extension" filled size={44} className="lg:size-9" />
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <p className="text-subhead text-label-2">{byline(plugin)}</p>
        <span className="flex flex-wrap items-center gap-2">
          <Badge tone={label.tone}>{label.text}</Badge>
          {plugin.state === 'running' && plugin.healthy === false && <Badge tone="orange">Not answering</Badge>}
        </span>
        {plugin.message && <p className="text-footnote wrap-break-word text-label-2">{plugin.message}</p>}
        {openable.length > 0 && (
          <span className="mt-1 flex flex-wrap gap-2">
            {openable.map((port) => (
              <a
                key={port.key}
                href={port.url}
                target="_blank"
                rel="noreferrer"
                aria-disabled={plugin.state !== 'running'}
                className="pressable inline-flex h-8 items-center gap-1 rounded-full bg-tint-fill px-4 text-subhead font-semibold text-white no-underline aria-disabled:pointer-events-none aria-disabled:opacity-40"
              >
                <Icon name="open_in_new" size={16} />
                {openable.length > 1 ? `Open ${port.name}` : 'Open'}
              </a>
            ))}
          </span>
        )}
      </div>
    </div>
  );
}

/** The Running switch: stopped plugins stay stopped, also after a restart. */
function RunningSection({ plugin }: { plugin: Plugin }) {
  const { setPluginRunning } = useDevice();
  const on = plugin.state === 'running' || plugin.state === 'starting';
  const footer =
    plugin.state === 'crashed'
      ? 'It stopped working several times in a row. Look at its log, then turn it on again.'
      : 'When on, it starts again whenever RasBeacon does.';

  return (
    <Section footer={footer}>
      <SwitchRow
        leading={{ tile: 'play_arrow', color: 'green' }}
        title="Running"
        checked={on}
        disabled={isBusy(plugin.state)}
        onChange={(running) => setPluginRunning(plugin.id, running)}
      />
    </Section>
  );
}

function SettingsForm({ plugin }: { plugin: Plugin }) {
  const { savePluginSettings, status } = useDevice();
  const form = useForm(draftOf(plugin.ports, plugin.settings, plugin.drive), (draft) =>
    savePluginSettings(plugin.id, choicesOf(draft)),
  );
  if (plugin.ports.length === 0 && plugin.settings.length === 0 && !plugin.usesDrive) return null;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <PluginFields
        ports={plugin.ports}
        settings={plugin.settings}
        usesDrive={plugin.usesDrive}
        driveOptional={plugin.driveOptional}
        draft={form.values}
        onChange={(draft) => form.update(() => draft)}
        errors={form.fieldErrors}
        host={status.data?.hotspot.lanAddress}
      />
      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={form.pending}
        error={form.error}
        success={form.saved && 'Saved.'}
        note={plugin.state === 'running' ? 'Saving restarts it.' : undefined}
      />
    </form>
  );
}

function PermissionsSection({ plugin }: { plugin: Plugin }) {
  return (
    <Section title="It can">
      {plugin.permissions.length === 0 ? (
        <Row leading={{ icon: 'block' }} title="Only answer on its ports, and keep its own data" />
      ) : (
        plugin.permissions.map((permission) => (
          <Row
            key={permission.id}
            leading={{ icon: PERMISSION_ICONS[permission.id] ?? 'info' }}
            title={permission.title}
            subtitle={permission.detail}
          />
        ))
      )}
    </Section>
  );
}

/** The last 500 lines it wrote, loaded when opened. */
function LogSection({ plugin }: { plugin: Plugin }) {
  const { pluginLogs } = useDevice();
  const [lines, setLines] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setError(null);
    try {
      setLines(await pluginLogs(plugin.id));
    } catch (problem) {
      setError(errorText(problem));
    }
  }

  return (
    <Section footer="Kept in memory until it or RasBeacon restarts.">
      <Disclosure
        title="Log"
        subtitle="Its last 500 lines"
        leading={{ icon: 'article' }}
        content="rows"
        onToggle={(open) => {
          if (open) void load();
        }}
      >
        <div className="row flex-col items-stretch gap-2">
          {error ? (
            <p className="text-subhead text-red">{error}</p>
          ) : lines === null ? (
            <p className="text-subhead text-label-2">Loading…</p>
          ) : lines.length === 0 ? (
            <p className="text-subhead text-label-2">Nothing yet.</p>
          ) : (
            <pre className="selectable max-h-80 overflow-auto overscroll-contain rounded-lg bg-fill-2 p-3 font-mono text-footnote whitespace-pre-wrap wrap-anywhere text-label">
              {lines.join('\n')}
            </pre>
          )}
        </div>
        <ButtonRow action={load} busyLabel="Loading…">
          Refresh
        </ButtonRow>
      </Disclosure>
    </Section>
  );
}

function AboutSection({ plugin }: { plugin: Plugin }) {
  return (
    <Section title="Details">
      <Row title="Version" value={plugin.version} />
      <Row title="From" value={<BreakableText>{sourceLabel(plugin.source)}</BreakableText>} />
      <Row
        title="Signature"
        value={
          plugin.signed ? (
            <span className="font-mono">{plugin.keyId}</span>
          ) : (
            <span className="text-orange">Not signed</span>
          )
        }
      />
      <Row title="Address" value={<span className="font-mono">{plugin.address}</span>} />
      <Row title="Memory" value={`Up to ${plugin.memoryMb} MB`} />
      <Row title="Storage" value={formatBytes(plugin.storageMb * 1_000_000)} />
      {plugin.homepage && <LinkRow to={plugin.homepage} external title="Website" />}
    </Section>
  );
}

/**
 * Updates never install by themselves. A signed plugin updates from the catalog (it must be
 * signed by the same key); an unsigned one from a file.
 */
function UpdateSection({ plugin }: { plugin: Plugin }) {
  const { previewPluginUpdate } = useDevice();
  const [preview, setPreview] = useState<PluginPreview | null>(null);

  const footer = plugin.updateVersion
    ? plugin.canUpdate
      ? `Version ${plugin.updateVersion} is in the catalog. Its settings and data are kept.`
      : `Version ${plugin.updateVersion} is in the catalog. This plugin isn't signed, so install its updates from a file.`
    : plugin.signed
      ? 'Updates must be signed by the same key as this version.'
      : 'An update keeps its settings and data.';

  return (
    <Section title="Update" footer={footer}>
      {plugin.updateVersion && plugin.canUpdate && (
        <ButtonRow
          leading={{ tile: 'system_update_alt', color: 'blue' }}
          action={async () => setPreview(await previewPluginUpdate(plugin.id))}
          busyLabel="Downloading…"
        >
          Update to {plugin.updateVersion}
        </ButtonRow>
      )}
      <InstallFromFile label="Update from a file" />
      {preview && <InstallSheet preview={preview} onDone={() => setPreview(null)} />}
    </Section>
  );
}

/** Removing deletes its image; its data may be kept for installing it again later. */
function RemoveSection({ plugin }: { plugin: Plugin }) {
  const { removePlugin } = useDevice();
  const navigate = useNavigate();
  const [asking, setAsking] = useState(false);
  const [keepData, setKeepData] = useState(false);

  return (
    <Section>
      <ButtonRow tone="danger" onClick={() => setAsking(true)}>
        Remove {plugin.name}
      </ButtonRow>
      {asking && (
        <AlertDialog
          title={`Remove ${plugin.name}?`}
          tone="danger"
          confirmLabel="Remove"
          busyLabel="Removing…"
          onConfirm={async () => {
            await removePlugin(plugin.id, keepData);
            navigate('/plugins');
          }}
          onClose={() => setAsking(false)}
        >
          <p>It stops, and its image is deleted.</p>
          <label className="mt-3 flex cursor-pointer items-center gap-3 text-left">
            <span id={`${plugin.id}-keep`} className="flex-1 text-subhead text-label">
              Keep its data, to use it if you install it again
            </span>
            <FormSwitch checked={keepData} onChange={setKeepData} labelledBy={`${plugin.id}-keep`} />
          </label>
        </AlertDialog>
      )}
    </Section>
  );
}
