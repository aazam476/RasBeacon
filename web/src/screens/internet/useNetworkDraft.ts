import { useDevice } from '@/data/device';
import type { SavedNetwork, UplinkSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';

/**
 * The list of Wi-Fi networks to join, as edited on the Internet page. Changes collect here and
 * are saved together, because the router stores the whole list at once.
 */
export function useNetworkDraft(settings: UplinkSettings) {
  const { saveWifiNetworks } = useDevice();
  const form = useForm(settings.networks, saveWifiNetworks);
  const networks = form.values;

  /** Applies a change to the list; old error messages about list positions no longer apply. */
  function change(edit: (list: SavedNetwork[]) => SavedNetwork[]) {
    form.update(edit, Object.keys(form.fieldErrors));
  }

  return {
    networks,
    dirty: form.dirty,
    saved: form.saved,
    pending: form.pending,
    error: form.error,
    /** Error message about the network at a list position, from the last save. */
    errorAt: (index: number): string | undefined => form.fieldErrors[`networks.${index}`],
    save: form.submit,
    discard: form.discard,
    add: (network: SavedNetwork) => change((list) => [...list, network]),
    remove: (index: number) => change((list) => list.filter((_, i) => i !== index)),
    /** Moves the network at `index` one place up (-1) or down (+1). */
    move: (index: number, delta: -1 | 1) =>
      change((list) => {
        const target = index + delta;
        if (target < 0 || target >= list.length) return list;
        const next = [...list];
        [next[index], next[target]] = [next[target], next[index]];
        return next;
      }),
    /** A new password for a saved network; "" keeps the saved one. */
    setPassword: (index: number, password: string) =>
      change((list) => list.map((network, i) => (i === index ? { ...network, password } : network))),
    /** Join this network with a random MAC address instead of the Pi's own. */
    setRandomMac: (index: number, randomMac: boolean) =>
      change((list) => list.map((network, i) => (i === index ? { ...network, randomMac } : network))),
  };
}

export type NetworkDraft = ReturnType<typeof useNetworkDraft>;
