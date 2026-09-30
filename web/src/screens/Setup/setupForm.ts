// The setup wizard's form: its fields and how they are checked.
// Kept separate from React so it can be unit-tested (see setupForm.test.ts).

import type { Band, ClaimRequest } from "../../api/types";
import {
  checkAdminPassword,
  checkPasswordsMatch,
  checkSsid,
  checkWifiPassword,
} from "../../shared/validation";

export interface SetupFormValues {
  adminPassword: string;
  adminPasswordConfirm: string;
  hotspotSsid: string;
  hotspotPassword: string;
  hotspotBand: Band;
}

/** An error message per field; a missing key means the field is fine. */
export type SetupFormErrors = Partial<Record<keyof SetupFormValues, string>>;

/** Checks every field and returns the problems found. */
export function validateSetupForm(values: SetupFormValues): SetupFormErrors {
  const errors: SetupFormErrors = {};
  const admin = checkAdminPassword(values.adminPassword);
  if (admin) errors.adminPassword = admin;
  const confirm = checkPasswordsMatch(values.adminPassword, values.adminPasswordConfirm);
  if (confirm) errors.adminPasswordConfirm = confirm;
  const ssid = checkSsid(values.hotspotSsid);
  if (ssid) errors.hotspotSsid = ssid;
  const wifi = checkWifiPassword(values.hotspotPassword);
  if (wifi) errors.hotspotPassword = wifi;
  return errors;
}

/** Turns the form into the API request (the confirmation box is not sent). */
export function toClaimRequest(values: SetupFormValues): ClaimRequest {
  return {
    adminPassword: values.adminPassword,
    hotspotSsid: values.hotspotSsid,
    hotspotPassword: values.hotspotPassword,
    hotspotBand: values.hotspotBand,
  };
}
