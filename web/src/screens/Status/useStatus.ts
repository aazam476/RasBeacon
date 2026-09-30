// Logic for the status page: loads the device status and refreshes it
// every 10 seconds so the list of connected devices stays current.

import { getStatus } from "../../api/client";
import type { Status } from "../../api/types";
import { useLoad } from "../../shared/useLoad";

export interface StatusLogic {
  status: Status | undefined;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

export function useStatus(): StatusLogic {
  const { data, error, loading, reload } = useLoad(getStatus, 10);
  return { status: data, error, loading, reload };
}
