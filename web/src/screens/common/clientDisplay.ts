import type { Client, Status } from '@/data/types';
import { formatBand, formatClientRate, formatDbm, formatDuration } from '@/lib/format';

/** A device on one of RasBeacon's two Wi-Fi networks. */
export interface NetworkClient extends Client {
  network: 'hotspot' | 'guest';
}

/** The devices of the hotspot and of the guest network in one list, each marked with its network. */
export function allClients(status: Status): NetworkClient[] {
  return [
    ...status.clients.map((client) => ({ ...client, network: 'hotspot' as const })),
    ...status.guest.clients.map((client) => ({ ...client, network: 'guest' as const })),
  ];
}

/** Which of RasBeacon's networks the device is on. */
export function networkName(client: NetworkClient): string {
  return client.network === 'guest' ? 'Guest network' : 'Your network';
}

/** The device's name, or its MAC address when it didn't send one. */
export function clientName(client: Client): string {
  return client.hostname || client.mac;
}

export interface ClientDetail {
  label: string;
  value: string;
  /** Red for a stopped reading (a paused device's traffic). */
  alert?: boolean;
}

/**
 * Address, traffic, radio and time readings for a device. A paused device shows its stopped
 * traffic and how long it has been paused instead.
 */
export function clientDetails(client: Client): ClientDetail[] {
  const address = { label: 'IP address', value: client.ip || 'waiting for address' };
  const radio = { label: 'Radio', value: `${formatBand(client.band)}, ${formatDbm(client.signalDbm)}` };
  const mac = { label: 'MAC address', value: client.mac };

  if (!client.internetAccess) {
    return [
      address,
      { label: 'Traffic', value: 'Stopped', alert: true },
      radio,
      {
        label: 'Paused',
        value: client.pausedSeconds ? `${formatDuration(client.pausedSeconds)} ago` : 'just now',
      },
      mac,
    ];
  }

  return [
    address,
    { label: 'Traffic', value: formatClientRate(client) },
    radio,
    { label: 'Connected for', value: formatDuration(client.connectedSeconds) },
    mac,
  ];
}
