// Internet page: USB Ethernet adapters, extra wired ports for devices.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen } from '../render';

describe('USB Ethernet', () => {
  it('isn’t shown without an adapter', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(screen.queryByRole('heading', { name: 'USB Ethernet' })).toBeNull();
  });

  it('says what one adapter is doing', () => {
    const device = fakeDevice((router) => {
      router.status.lanPorts = [{ name: 'enx00e04c680001', cablePlugged: true, connected: true }];
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(screen.getByRole('heading', { name: 'USB Ethernet' })).toBeTruthy();
    expect(screen.getByText('Adapter')).toBeTruthy();
    expect(screen.getByText('For devices')).toBeTruthy();
  });

  it('names each of several, and warns about one that leads to a network', () => {
    const device = fakeDevice((router) => {
      router.status.lanPorts = [
        { name: 'enx1', cablePlugged: true, connected: false, foreignDhcpServer: '192.168.178.1' },
        { name: 'enx2', cablePlugged: true, connected: false },
        { name: 'enx3', cablePlugged: false, connected: false },
      ];
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(screen.getByText('Adapter enx1')).toBeTruthy();
    expect(screen.getByText('Not used')).toBeTruthy();
    expect(screen.getByText('Checking…')).toBeTruthy();
    // The Ethernet port's badge says "No cable" too.
    expect(screen.getAllByText('No cable')).toHaveLength(2);
    const alerts = screen.getAllByRole('alert').map((alert) => alert.textContent);
    expect(alerts.some((text) => text?.includes('adapter enx1 (192.168.178.1)'))).toBe(true);
  });
});
