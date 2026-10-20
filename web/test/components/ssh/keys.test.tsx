// SSH page: adding public keys.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SshScreen } from '@/screens/ssh/SshScreen';
import { fakeDevice, renderScreen } from '../render';

describe('SSH keys', () => {
  it('offers the key file first, then pasting, as on the VPN page', () => {
    renderScreen(<SshScreen />, fakeDevice());
    const file = screen.getByLabelText(/^Add from a file/);
    const paste = screen.getByLabelText(/^Or paste its contents/);
    // The file field comes before the text box in the page.
    expect(file.compareDocumentPosition(paste) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('saves the pasted key with SSH turned on', async () => {
    const device = fakeDevice((router) => {
      router.sshSettings = { enabled: false, authorizedKeys: '' };
    });
    const { user } = renderScreen(<SshScreen />, device);
    await user.click(screen.getByLabelText('Turn on SSH'));
    await user.type(screen.getByLabelText(/^Or paste its contents/), 'ssh-ed25519 AAAA me@laptop');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveSshSettings).toHaveBeenCalledWith({
      enabled: true,
      authorizedKeys: 'ssh-ed25519 AAAA me@laptop',
    });
  });
});
