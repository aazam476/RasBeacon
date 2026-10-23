import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { VPNSettings } from '@/data/types';
import {
  AlertDialog,
  BreakableText,
  Choice,
  ChoiceList,
  EditButton,
  IconButton,
  RenameSheet,
  Row,
  Section,
} from '@/ui';

/**
 * The imported WireGuard profiles: picking one makes it the tunnel in use; each can be renamed
 * (the pencil, shown on hover with a mouse) and removed.
 */
export function ProfilesSection({ settings }: { settings: VPNSettings }) {
  const { setActiveVpnProfile, removeVpnProfile, renameVpnProfile } = useDevice();
  const [removing, setRemoving] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);

  return (
    <Section title="WireGuard profiles" footer="The checked profile is the one in use.">
      {settings.profiles.length === 0 ? (
        <Row title={<span className="text-label-2">No profiles yet. Import one below.</span>} />
      ) : (
        <ChoiceList ariaLabel="WireGuard profile" value={settings.activeProfile} onChange={setActiveVpnProfile}>
          {settings.profiles.map((profile) => (
            <Choice
              key={profile.name}
              value={profile.name}
              title={<BreakableText>{profile.name}</BreakableText>}
              description={profile.endpoint ? <BreakableText>{profile.endpoint}</BreakableText> : 'No server set'}
              mono
              icon="description"
              trailing={
                <>
                  <EditButton label={`Rename ${profile.name}`} onClick={() => setRenaming(profile.name)} />
                  <IconButton
                    icon="delete"
                    ariaLabel={`Remove ${profile.name}`}
                    className="hover:text-red"
                    onClick={() => setRemoving(profile.name)}
                  />
                </>
              }
            />
          ))}
        </ChoiceList>
      )}

      {renaming && (
        <RenameSheet
          title="Rename profile"
          label="Name"
          initial={renaming}
          maxLength={64}
          onSave={(name) => renameVpnProfile(renaming, name)}
          onClose={() => setRenaming(null)}
        />
      )}
      {removing && (
        <AlertDialog
          title={`Remove ${removing}?`}
          tone="danger"
          confirmLabel="Remove"
          busyLabel="Removing…"
          onConfirm={() => removeVpnProfile(removing)}
          onClose={() => setRemoving(null)}
        >
          <p>
            Its keys are deleted from RasBeacon; to use it again, import the file again.
            {removing === settings.activeProfile &&
              ' It is the profile in use, so the VPN switches to the next profile, or turns off if none is left.'}
          </p>
        </AlertDialog>
      )}
    </Section>
  );
}
