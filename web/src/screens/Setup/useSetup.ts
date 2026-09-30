// Logic for the first-time setup wizard. No markup here: SetupView.tsx
// draws the screen from what this hook returns.

import { useState } from "react";
import { ApiError, claim } from "../../api/client";
import type { ClaimResponse, SetupState } from "../../api/types";
import {
  toClaimRequest,
  validateSetupForm,
  type SetupFormErrors,
  type SetupFormValues,
} from "./setupForm";

export interface SetupLogic {
  values: SetupFormValues;
  /** Changes one field, e.g. setField("hotspotSsid", "My Wi-Fi"). */
  setField: <K extends keyof SetupFormValues>(field: K, value: SetupFormValues[K]) => void;
  /** Field problems. Only filled in after the first "Finish setup" press. */
  errors: SetupFormErrors;
  /** A problem not tied to one field (for example "can't reach RasBeacon"). */
  formError: string | null;
  submitting: boolean;
  /** Set once setup succeeded: tells the owner which network to join next. */
  result: ClaimResponse | null;
  submit: () => void;
  /** True when setup.txt protected the setup network with a password. */
  setupNetworkSecured: boolean;
  /** Minutes until the setup network closes, or null if unknown. */
  minutesLeft: number | null;
}

export function useSetup(state: SetupState): SetupLogic {
  const [values, setValues] = useState<SetupFormValues>({
    adminPassword: "",
    adminPasswordConfirm: "",
    hotspotSsid: state.suggestedHotspotSsid ?? "",
    hotspotPassword: state.suggestedHotspotPassword ?? "",
    hotspotBand: "2.4GHz",
  });
  const [showErrors, setShowErrors] = useState(false);
  const [serverErrors, setServerErrors] = useState<SetupFormErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<ClaimResponse | null>(null);

  const setField: SetupLogic["setField"] = (field, value) => {
    setValues((current) => ({ ...current, [field]: value }));
    setServerErrors((current) => ({ ...current, [field]: undefined }));
  };

  const localErrors = validateSetupForm(values);

  async function submit() {
    setShowErrors(true);
    setFormError(null);
    if (Object.keys(localErrors).length > 0) return;

    setSubmitting(true);
    try {
      setResult(await claim(toClaimRequest(values)));
    } catch (err) {
      if (err instanceof ApiError && err.field) {
        // The device rejected one field (e.g. a too-common password).
        setServerErrors({ [err.field]: err.message });
      } else {
        setFormError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return {
    values,
    setField,
    errors: showErrors
      ? { ...localErrors, ...definedOnly(serverErrors) }
      : definedOnly(serverErrors),
    formError,
    submitting,
    result,
    submit: () => void submit(),
    setupNetworkSecured: state.setupNetworkSecured,
    minutesLeft:
      state.setupClosesInSeconds === undefined ? null : Math.ceil(state.setupClosesInSeconds / 60),
  };
}

/** Removes keys whose value is undefined. */
function definedOnly(errors: SetupFormErrors): SetupFormErrors {
  return Object.fromEntries(Object.entries(errors).filter(([, v]) => v !== undefined));
}
