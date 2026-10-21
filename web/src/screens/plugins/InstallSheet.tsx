import { useRef, useState } from 'react';
import { useDevice } from '@/data/device';
import type { PluginKeptData, PluginPreview } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { Badge, FormSwitchRow, Icon, Notice, Row, Section, Sheet } from '@/ui';
import { PluginFields } from './PluginFields';
import { choicesOf, draftOf, PERMISSION_ICONS, sourceLabel } from './pluginDisplay';

interface InstallSheetProps {
  preview: PluginPreview;
  /** Called after installing, with the plugin's ID, or when the sheet closes. */
  onDone: (installedId?: string) => void;
}

/**
 * Before installing: what the plugin is, who signed it, what it will be able to do (as a phone
 * asks for permissions), and the choices it needs. Nothing is installed until Install is pressed.
 */
export function InstallSheet({ preview, onDone }: InstallSheetProps) {
  const { installPlugin, cancelPluginInstall, status } = useDevice();
  const [draft, setDraft] = useState(() => draftOf(preview.ports, preview.settings, preview.drive));
  // Off until the owner says otherwise: nothing shows the kept data belongs to this plugin.
  const [useKeptData, setUseKeptData] = useState(false);
  // Closing after Install isn't a cancel.
  const installed = useRef(false);
  const host = status.data?.hotspot.lanAddress;
  const newPermissions = new Set(preview.newPermissions ?? []);

  return (
    <Sheet
      size="lg"
      title={preview.update ? `Update ${preview.name}` : `Install ${preview.name}`}
      confirmLabel={preview.update ? 'Update' : 'Install'}
      busyLabel={preview.update ? 'Updating…' : 'Installing…'}
      onConfirm={async () => {
        await installPlugin(preview.stagingId, choicesOf(draft), useKeptData);
        installed.current = true;
        onDone(preview.id);
      }}
      onClose={() => {
        if (installed.current) return;
        void cancelPluginInstall(preview.stagingId);
        onDone();
      }}
    >
      {(errors) => (
        <>
          <PluginHero preview={preview} />

          {preview.warnings?.map((warning) => (
            <Notice key={warning} tone="warning">
              <p>{warning}</p>
            </Notice>
          ))}

          <Section
            level={3}
            title="It will be able to"
            footer="It runs on its own, with its own address, and can't change RasBeacon's settings or see your passwords."
          >
            {preview.permissions.length === 0 ? (
              <Row
                leading={{ icon: 'block' }}
                title="Nothing more"
                subtitle="Only answer on its ports, and keep its own data."
              />
            ) : (
              preview.permissions.map((permission) => (
                <Row
                  key={permission.id}
                  leading={{ icon: PERMISSION_ICONS[permission.id] ?? 'info' }}
                  title={
                    <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
                      {permission.title}
                      {newPermissions.has(permission.id) && <Badge tone="orange">New</Badge>}
                    </span>
                  }
                  subtitle={permission.detail}
                />
              ))
            )}
          </Section>

          <PluginFields
            level={3}
            ports={preview.ports}
            settings={preview.settings}
            usesDrive={preview.usesDrive}
            driveOptional={preview.driveOptional}
            draft={draft}
            onChange={setDraft}
            errors={errors}
            host={host}
          />

          {preview.keptData && (
            <KeptDataSection
              kept={preview.keptData}
              signed={preview.signed}
              checked={useKeptData}
              onChange={setUseKeptData}
            />
          )}

          <Section level={3} title="What it uses" footer="Limits it can't go past. Its data stays when you update it.">
            <Row title="Memory" value={`Up to ${preview.memoryMb} MB`} />
            <Row title="Processor" value={`Up to ${formatCores(preview.cpus)}`} />
            <Row title="Storage" value={formatBytes(preview.storageMb * 1_000_000)} />
            <Row
              title="Download"
              value={
                preview.downloadSize > 0
                  ? formatBytes(preview.downloadSize)
                  : preview.downloadSize < 0
                    ? 'Size unknown'
                    : 'None (in the file)'
              }
            />
          </Section>

          {preview.metered && preview.downloadSize !== 0 && (
            <Notice tone="warning" title="Metered connection">
              <p>
                {preview.downloadSize > 0
                  ? `Installing downloads ${formatBytes(preview.downloadSize)} through your metered connection.`
                  : 'Installing downloads its image through your metered connection; its size is unknown.'}
              </p>
            </Notice>
          )}
        </>
      )}
    </Sheet>
  );
}

function formatCores(cpus: number): string {
  if (cpus === 1) return 'one core';
  if (cpus < 1) return `${Math.round(cpus * 100)}% of a core`;
  return `${cpus} cores`;
}

/** The plugin, large, with who made and signed it. */
function PluginHero({ preview }: { preview: PluginPreview }) {
  return (
    <div className="flex flex-col items-center gap-2 px-2 pt-2 pb-1 text-center">
      <img src={preview.icon} alt="" draggable={false} className="size-20 rounded-[18px] shadow-sm" />
      <h3 className="text-title2 wrap-break-word text-label">{preview.name}</h3>
      <p className="text-subhead text-label-2">
        {preview.update ? `${preview.fromVersion} → ${preview.version}` : `Version ${preview.version}`}
        {preview.author && ` · by ${preview.author}`}
      </p>
      {preview.description && (
        <p className="max-w-96 text-body text-pretty wrap-break-word text-label">{preview.description}</p>
      )}
      <p className="flex items-center gap-1.5 text-footnote text-label-2">
        {preview.signed ? (
          <>
            <Icon name="verified_user" filled size={16} className="text-green" />
            <span>
              Signed · <span className="font-mono">{preview.keyId}</span>
            </span>
          </>
        ) : (
          <>
            <Icon name="gpp_maybe" filled size={16} className="text-orange" />
            <span>Not signed: install it only if you trust where it came from.</span>
          </>
        )}
      </p>
      <p className="text-footnote wrap-anywhere text-label-3">{sourceLabel(preview.source)}</p>
    </div>
  );
}

/**
 * Data kept from an earlier install with this ID, which isn't known to be this plugin's: the
 * switch gives it to this one, off by default, with who signed the earlier one.
 */
function KeptDataSection({
  kept,
  signed,
  checked,
  onChange,
}: {
  kept: PluginKeptData;
  signed: boolean;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const earlier = kept.name ? `the earlier ${kept.name} install` : 'an earlier install';
  let why: string;
  if (!kept.name && !kept.keyId) why = "It was kept by RasBeacon 0.1, which didn't note whose it was.";
  else if (!kept.keyId) why = "That one wasn't signed, so nothing shows this is the same plugin.";
  else if (!signed) why = "This one isn't signed, so it may be another author's plugin.";
  else why = 'This one is signed with another key, so it may be another author’s plugin.';

  return (
    <Section
      level={3}
      title="Kept data"
      footer={`${why} Turn this on only if you trust it with that plugin's settings, passwords and files.`}
    >
      <FormSwitchRow
        leading={{ icon: 'folder' }}
        label={`Use the data kept from ${earlier}`}
        description={
          kept.keyId ? (
            <>
              Signed · <span className="font-mono wrap-anywhere">{kept.keyId}</span>
            </>
          ) : (
            'Not signed'
          )
        }
        checked={checked}
        onChange={onChange}
      />
    </Section>
  );
}
