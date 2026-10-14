import { useEffect, useState } from 'react';
import { useDevice } from '@/data/device';
import type { PluginDrive, PluginPort, PluginSetting } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { FormSwitchRow, PasswordField, Section, SelectField, TextField } from '@/ui';
import type { ChoicesDraft } from './pluginDisplay';

interface PluginFieldsProps {
  ports: PluginPort[];
  settings: PluginSetting[];
  usesDrive: boolean;
  driveOptional?: boolean;
  draft: ChoicesDraft;
  onChange: (draft: ChoicesDraft) => void;
  errors: Readonly<Record<string, string | undefined>>;
  /** RasBeacon's address on the hotspot, for the ports' footer. */
  host?: string;
  /** Section headings one level down, inside a sheet. */
  level?: 2 | 3;
}

/** Typing settings for names and ports: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

/**
 * A plugin's form, drawn from its manifest: the ports on RasBeacon, its settings, and the USB
 * drive it uses. Used by the install sheet and the plugin's page.
 */
export function PluginFields({
  ports,
  settings,
  usesDrive,
  driveOptional,
  draft,
  onChange,
  errors,
  host,
  level = 2,
}: PluginFieldsProps) {
  const setPort = (key: string, value: string) => onChange({ ...draft, ports: { ...draft.ports, [key]: value } });
  const setSetting = (key: string, value: string) =>
    onChange({ ...draft, settings: { ...draft.settings, [key]: value } });

  return (
    <>
      {ports.length > 0 && (
        <Section
          level={level}
          title="Ports on RasBeacon"
          footer={`Devices on your hotspot reach it at ${host ?? 'RasBeacon'}:<port>. Guests and the internet can't.`}
        >
          {ports.map((port) => (
            <TextField
              key={port.key}
              label={port.name}
              labelNote={`(its port ${port.container}/${port.protocol.toUpperCase()})`}
              inputMode="numeric"
              maxLength={5}
              value={draft.ports[port.key] ?? ''}
              onChange={(value) => setPort(port.key, value.replace(/\D/g, ''))}
              error={errors[`ports.${port.key}`]}
              {...PLAIN_INPUT}
            />
          ))}
        </Section>
      )}

      {settings.length > 0 && (
        <Section level={level} title="Settings">
          {settings.map((setting) => (
            <SettingField
              key={setting.key}
              setting={setting}
              value={draft.settings[setting.key] ?? ''}
              onChange={(value) => setSetting(setting.key, value)}
              error={errors[`settings.${setting.key}`]}
            />
          ))}
        </Section>
      )}

      {usesDrive && (
        <DriveField
          level={level}
          optional={driveOptional}
          value={draft.drive}
          onChange={(drive) => onChange({ ...draft, drive })}
          error={errors.drive}
        />
      )}
    </>
  );
}

interface SettingFieldProps {
  setting: PluginSetting;
  value: string;
  onChange: (value: string) => void;
  error?: string;
}

function SettingField({ setting, value, onChange, error }: SettingFieldProps) {
  const label = setting.unit ? `${setting.label} (${setting.unit})` : setting.label;
  switch (setting.type) {
    case 'switch':
      return (
        <FormSwitchRow
          label={setting.label}
          description={error ?? setting.help}
          checked={value === 'true'}
          onChange={(checked) => onChange(String(checked))}
        />
      );
    case 'select':
      return (
        <SelectField
          label={setting.label}
          hint={setting.help}
          options={setting.options ?? []}
          value={value}
          onChange={onChange}
          error={error}
        />
      );
    case 'password':
      return (
        <PasswordField
          label={setting.label}
          labelNote={setting.hasValue ? '(leave empty to keep it)' : undefined}
          placeholder={setting.hasValue ? 'Unchanged' : undefined}
          hint={setting.help}
          autoComplete="new-password"
          value={value}
          onChange={onChange}
          error={error}
        />
      );
    case 'number':
      return (
        <TextField
          label={label}
          labelNote={
            setting.min !== undefined && setting.max !== undefined ? `(${setting.min} to ${setting.max})` : undefined
          }
          hint={setting.help}
          inputMode="decimal"
          value={value}
          onChange={onChange}
          error={error}
          {...PLAIN_INPUT}
        />
      );
    default:
      return (
        <TextField label={label} hint={setting.help} value={value} onChange={onChange} error={error} {...PLAIN_INPUT} />
      );
  }
}

interface DriveFieldProps {
  level: 2 | 3;
  optional?: boolean;
  value: string;
  onChange: (uuid: string) => void;
  error?: string;
}

/** The USB drive the plugin uses, from the drives plugged in now. */
function DriveField({ level, optional, value, onChange, error }: DriveFieldProps) {
  const { pluginDrives } = useDevice();
  const [drives, setDrives] = useState<PluginDrive[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let open = true;
    pluginDrives()
      .then((list) => open && setDrives(list))
      .catch((reason: unknown) => open && setProblem(reason instanceof Error ? reason.message : String(reason)));
    return () => {
      open = false;
    };
  }, [pluginDrives]);

  const options = [
    ...(optional || value === '' ? [{ value: '', label: optional ? 'None (its own storage)' : 'Choose a drive' }] : []),
    ...(drives ?? []).map((drive) => ({
      value: drive.uuid,
      label: `${drive.label || 'Untitled'} · ${formatBytes(drive.size)} ${drive.fsType}${drive.usedBy ? ` (used by ${drive.usedBy})` : ''}`,
    })),
  ];
  // A chosen drive that is unplugged now stays in the list.
  if (value && !options.some((option) => option.value === value)) {
    options.push({ value, label: 'The chosen drive (not plugged in)' });
  }

  return (
    <Section
      level={level}
      title="USB drive"
      footer="Only this plugin gets the drive. Nothing on it can run, so a drive from anywhere is safe to use."
    >
      <SelectField
        label="Drive"
        options={options}
        value={value}
        onChange={onChange}
        disabled={drives === null && !problem}
        hint={drives === null && !problem ? 'Looking for drives…' : (problem ?? undefined)}
        error={error}
      />
    </Section>
  );
}
