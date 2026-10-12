import type { NetworkClient } from '../common/clientDisplay';

export type ClientFilter = 'all' | '5ghz' | '2.4ghz' | 'guests' | 'blocked';

interface ClientFilterOption {
  value: ClientFilter;
  label: string;
  matches: (client: NetworkClient) => boolean;
}

/** The filters above the device list, in display order. */
export const CLIENT_FILTERS: readonly ClientFilterOption[] = [
  { value: 'all', label: 'All', matches: () => true },
  { value: '5ghz', label: '5 GHz', matches: (client) => client.band === '5GHz' },
  { value: '2.4ghz', label: '2.4 GHz', matches: (client) => client.band === '2.4GHz' },
  { value: 'guests', label: 'Guests', matches: (client) => client.network === 'guest' },
  { value: 'blocked', label: 'Paused', matches: (client) => !client.internetAccess },
];

export function filterClients(clients: NetworkClient[], filter: ClientFilter): NetworkClient[] {
  const option = CLIENT_FILTERS.find((candidate) => candidate.value === filter);
  return option ? clients.filter(option.matches) : clients;
}
