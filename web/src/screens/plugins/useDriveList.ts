import { useEffect, useState } from 'react';
import { useDevice } from '@/data/device';
import type { PluginDrive } from '@/data/types';
import { drivesErrorText } from './pluginDisplay';

/** How long to wait before asking for the drives again, while RasBeacon can't be reached. */
export const DRIVES_RETRY_MS = 5_000;

/**
 * The USB drives plugged in now, for a drive dropdown: `drives` is null until they are listed,
 * and `problem` says why they couldn't be. When RasBeacon can't be reached at all, there is no
 * problem to show under the dropdown (the note at the top of the page says so); it keeps
 * looking, and asks again every few seconds until the drives can be listed.
 *
 * `enabled` false lists nothing (an empty list), for a plugin that can't use drives.
 */
export function useDriveList(enabled = true): { drives: PluginDrive[] | null; problem: string | null } {
  const { pluginDrives } = useDevice();
  const [drives, setDrives] = useState<PluginDrive[] | null>(enabled ? null : []);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let open = true;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const list = () => {
      pluginDrives()
        .then((found) => {
          if (!open) return;
          setDrives(found);
          setProblem(null);
        })
        .catch((reason: unknown) => {
          if (!open) return;
          const text = drivesErrorText(reason);
          setProblem(text);
          if (text === null) retry = setTimeout(list, DRIVES_RETRY_MS);
        });
    };
    list();
    return () => {
      open = false;
      clearTimeout(retry);
    };
  }, [pluginDrives, enabled]);

  return { drives, problem };
}
