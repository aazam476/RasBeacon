import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { AdminPasswordForm } from '@/data/types';
import { ButtonRow, FormSuccess, PasswordField, Section, Sheet } from '@/ui';

const EMPTY: AdminPasswordForm = { currentPassword: '', newPassword: '', newPasswordConfirm: '' };

/** A new admin password, given the current one. Other devices are logged out. */
export function AdminPasswordSection() {
  const [changing, setChanging] = useState(false);
  const [changed, setChanged] = useState(false);

  return (
    <Section footer="Other devices logged in to these settings are logged out, and need the new password.">
      <ButtonRow
        leading={{ tile: 'key', color: 'gray' }}
        onClick={() => {
          setChanged(false);
          setChanging(true);
        }}
      >
        Change admin password
      </ButtonRow>
      {changed && (
        <div className="row">
          <FormSuccess>Admin password changed.</FormSuccess>
        </div>
      )}
      {changing && <AdminPasswordSheet onChanged={() => setChanged(true)} onClose={() => setChanging(false)} />}
    </Section>
  );
}

function AdminPasswordSheet({ onChanged, onClose }: { onChanged: () => void; onClose: () => void }) {
  const { changeAdminPassword } = useDevice();
  const [form, setForm] = useState(EMPTY);
  const set = (field: keyof AdminPasswordForm) => (value: string) =>
    setForm((current) => ({ ...current, [field]: value }));

  return (
    <Sheet
      title="Admin password"
      confirmLabel="Change"
      busyLabel="Changing…"
      onConfirm={async () => {
        await changeAdminPassword(form);
        onChanged();
      }}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section level={3}>
            <PasswordField
              label="Current password"
              autoComplete="current-password"
              data-autofocus
              value={form.currentPassword}
              onChange={set('currentPassword')}
              error={errors.currentPassword}
            />
          </Section>
          <Section level={3} footer="At least 10 characters.">
            <PasswordField
              label="New password"
              autoComplete="new-password"
              value={form.newPassword}
              onChange={set('newPassword')}
              error={errors.newPassword}
            />
            <PasswordField
              label="Type it again"
              autoComplete="new-password"
              value={form.newPasswordConfirm}
              onChange={set('newPasswordConfirm')}
              error={errors.newPasswordConfirm}
            />
          </Section>
        </>
      )}
    </Sheet>
  );
}
