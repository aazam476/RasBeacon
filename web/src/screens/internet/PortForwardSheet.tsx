import { useState } from 'react';
import { useDevice } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import type { PortForwardProtocol, PortForwardRule } from '@/data/types';
import { parsePorts, portsText, PROTOCOL_OPTIONS, ruleProblems } from '@/lib/portForward';
import { ButtonRow, FormSwitchRow, Notice, Section, SelectField, Sheet, TextField } from '@/ui';
import { clientName } from '../common/clientDisplay';

/** A choice in the "Goes to" menu: `device:<mac>` or `plugin:<id>`, or "" for none yet. */
type TargetKey = string;

function targetKey(rule: PortForwardRule | null): TargetKey {
  if (rule?.plugin) return `plugin:${rule.plugin}`;
  if (rule?.mac) return `device:${rule.mac}`;
  return '';
}

/**
 * Adds or changes one port forwarding rule (`rule` null to add). The whole list is saved with
 * the rule changed, as the router keeps it. A device without a fixed address gets its current
 * address as its fixed one first, so the rule always finds it.
 */
export function PortForwardSheet({
  rule,
  rules,
  onClose,
}: {
  rule: PortForwardRule | null;
  rules: PortForwardRule[];
  onClose: () => void;
}) {
  const { status, savePortForwarding, setFixedAddress } = useDevice();
  const [name, setName] = useState(rule?.name ?? '');
  const [protocol, setProtocol] = useState<PortForwardProtocol>(rule?.protocol ?? 'tcp');
  const [ports, setPorts] = useState(rule ? portsText(rule) : '');
  const [inside, setInside] = useState(rule?.insidePort ? String(rule.insidePort) : '');
  const [target, setTarget] = useState<TargetKey>(targetKey(rule));
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);

  // The menu: the hotspot's devices (guests can't be forwarded to), the device of this rule if it
  // isn't connected now, then the installed plugins.
  const clients = status.data?.clients ?? [];
  const deviceOptions = clients.map((client) => ({ value: `device:${client.mac}`, label: clientName(client) }));
  if (rule?.mac && !clients.some((client) => client.mac === rule.mac)) {
    deviceOptions.push({ value: `device:${rule.mac}`, label: rule.target ?? rule.mac });
  }
  const pluginOptions = (status.data?.plugins ?? []).map((plugin) => ({
    value: `plugin:${plugin.id}`,
    label: `${plugin.name} (plugin)`,
  }));
  const options = [{ value: '', label: 'Choose…' }, ...deviceOptions, ...pluginOptions];

  // The rule as typed, and what's wrong with it (only for fields that have something in them,
  // so a new form doesn't start covered in errors).
  const range = parsePorts(ports);
  // A range keeps its ports, so the inside port is left out (and hidden) then.
  const isRange = typeof range !== 'string' && range.to > range.from;
  const insidePort = isRange || inside.trim() === '' ? 0 : Number(inside.trim());
  const [kind, id] = target.split(/:(.*)/);
  const draft: PortForwardRule = {
    id: rule?.id ?? '',
    name: name.trim(),
    protocol,
    outsidePort: typeof range === 'string' ? 0 : range.from,
    outsidePortEnd: typeof range === 'string' || range.to === range.from ? undefined : range.to,
    insidePort: insidePort || undefined,
    mac: kind === 'device' ? id : undefined,
    plugin: kind === 'plugin' ? id : undefined,
    enabled,
  };
  const others = rules.filter((other) => other.id !== rule?.id);
  const problems = typeof range === 'string' ? {} : ruleProblems(draft, others);
  if (name.trim() === '') delete problems.name;
  if (target === '') delete problems.target;
  const portsProblem = ports.trim() === '' ? undefined : typeof range === 'string' ? range : problems.outsidePort;
  const insideProblem =
    !isRange && inside.trim() !== '' && !(/^\d{1,5}$/.test(inside.trim()) && insidePort >= 1 && insidePort <= 65535)
      ? 'Use a port from 1 to 65535, or leave it empty for the same port.'
      : problems.insidePort;
  const complete = name.trim() !== '' && ports.trim() !== '' && target !== '';
  const invalid = Object.values(problems).some(Boolean) || portsProblem !== undefined || insideProblem !== undefined;

  // A device that is here now without a fixed address gets the address it has now.
  const device = kind === 'device' ? clients.find((client) => client.mac === id) : undefined;
  const fixNow = enabled && device && !device.fixedAddress && device.ip ? device.ip : '';

  async function save() {
    if (fixNow && device) {
      try {
        await setFixedAddress(device.mac, fixNow);
      } catch (error) {
        // Shown under the menu, where the note about the address is.
        if (error instanceof FieldErrors) throw new FieldErrors({ target: Object.values(error.fields)[0] });
        throw error;
      }
    }
    const list = rule ? rules.map((other) => (other.id === rule.id ? draft : other)) : [...rules, draft];
    await savePortForwarding(list);
  }

  return (
    <Sheet
      title={rule ? 'Edit rule' : 'Add rule'}
      confirmLabel="Save"
      busyLabel="Saving…"
      confirmDisabled={!complete || invalid}
      onConfirm={save}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section level={3}>
            <TextField
              label="Name"
              data-autofocus={rule ? undefined : true}
              placeholder="Minecraft server"
              value={name}
              onChange={setName}
              maxLength={40}
              error={problems.name ?? errors.name}
            />
            <SelectField
              label="Protocol"
              options={PROTOCOL_OPTIONS}
              value={protocol}
              onChange={setProtocol}
              error={errors.protocol}
            />
            <TextField
              label="Outside port"
              placeholder="25565"
              hint="One port, or a range such as 27015-27030."
              value={ports}
              onChange={setPorts}
              error={portsProblem ?? errors.outsidePort}
              autoComplete="off"
              spellCheck={false}
            />
            <SelectField
              label="Goes to"
              options={options}
              value={target}
              onChange={setTarget}
              hint={
                fixNow
                  ? `Saving also gives this device the fixed address ${fixNow}, so the rule always finds it.`
                  : undefined
              }
              error={problems.target ?? errors.target}
            />
            {!isRange && (
              <TextField
                label="Inside port"
                labelNote="(optional)"
                inputMode="numeric"
                placeholder={typeof range === 'string' ? 'The same' : String(range.from)}
                hint="The port on the device, if it differs."
                value={inside}
                onChange={setInside}
                error={insideProblem ?? errors.insidePort}
                autoComplete="off"
              />
            )}
          </Section>
          <Section level={3} footer={errors.rules}>
            <FormSwitchRow label="On" checked={enabled} onChange={setEnabled} />
          </Section>
          {rule?.warning && (
            <Notice tone="warning">
              <p>{rule.warning}</p>
            </Notice>
          )}
          {rule && (
            <Section level={3}>
              <ButtonRow
                tone="danger"
                busyLabel="Deleting…"
                action={async () => {
                  await savePortForwarding(rules.filter((other) => other.id !== rule.id));
                  onClose();
                }}
              >
                Delete rule
              </ButtonRow>
            </Section>
          )}
        </>
      )}
    </Sheet>
  );
}
