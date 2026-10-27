import { useState } from 'react';
import { useDevice } from '@/data/device';
import { fixedAddressProblem } from '@/lib/fixedAddress';
import { FormSwitchRow, Section, Sheet, TextField } from '@/ui';
import type { NetworkClient } from '../common/clientDisplay';

/**
 * "Always use this address" for one device: the switch, and the address it then always gets.
 * It starts with the address the device has now, so turning the switch on keeps it where it is.
 */
export function FixedAddressSheet({ client, onClose }: { client: NetworkClient; onClose: () => void }) {
  const { status, setFixedAddress } = useDevice();
  const [fixed, setFixed] = useState(client.fixedAddress !== undefined);
  const [address, setAddress] = useState(client.fixedAddress ?? client.ip ?? '');
  const clients = status.data?.clients ?? [];
  const problem = fixed ? fixedAddressProblem(address, client.mac, clients, status.data?.hotspot.lanAddress) : null;
  const unchanged = fixed ? address.trim() === client.fixedAddress : client.fixedAddress === undefined;

  return (
    <Sheet
      title="Fixed address"
      confirmLabel="Save"
      busyLabel="Saving…"
      confirmDisabled={unchanged || problem !== null || (fixed && address.trim() === '')}
      onConfirm={() => setFixedAddress(client.mac, fixed ? address.trim() : '')}
      onClose={onClose}
    >
      {(errors) => (
        <Section
          level={3}
          footer={
            <>
              Other devices, and port forwarding, can then find this one at the same address. It gets the new address
              the next time it connects. Phones and laptops often use a private address for each Wi-Fi network; this
              follows <span className="font-mono wrap-anywhere">{client.mac}</span>, the one it uses on this hotspot.
            </>
          }
        >
          <FormSwitchRow label="Always use this address" checked={fixed} onChange={setFixed} />
          {fixed && (
            <TextField
              label="Address"
              data-autofocus
              inputMode="decimal"
              placeholder={client.ip ?? '10.87.0.50'}
              value={address}
              onChange={setAddress}
              error={problem ?? errors.address}
              autoComplete="off"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
            />
          )}
        </Section>
      )}
    </Sheet>
  );
}
