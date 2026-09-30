import { describe, expect, it } from "vitest";
import { toClaimRequest, validateSetupForm, type SetupFormValues } from "./setupForm";

const good: SetupFormValues = {
  adminPassword: "tangerine-orbit-42",
  adminPasswordConfirm: "tangerine-orbit-42",
  hotspotSsid: "RasBeacon-4F2A",
  hotspotPassword: "kq7m-2xvd-h8pt-rn4w",
  hotspotBand: "2.4GHz",
};

describe("validateSetupForm", () => {
  it("accepts a complete, valid form", () => {
    expect(validateSetupForm(good)).toEqual({});
  });

  it("reports each problem on its own field", () => {
    const errors = validateSetupForm({
      ...good,
      adminPassword: "short",
      adminPasswordConfirm: "different",
      hotspotPassword: "1234567",
    });
    expect(Object.keys(errors).sort()).toEqual([
      "adminPassword",
      "adminPasswordConfirm",
      "hotspotPassword",
    ]);
  });
});

describe("toClaimRequest", () => {
  it("does not send the confirmation box", () => {
    expect(toClaimRequest(good)).not.toHaveProperty("adminPasswordConfirm");
  });
});
