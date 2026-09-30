import { describe, expect, it } from "vitest";
import { decidePhase } from "./useApp";
import type { SetupState } from "../api/types";

const base: SetupState = {
  mode: "normal",
  claimed: true,
  setupNetworkSecured: false,
  deviceShortId: "4F2A",
  lanAddress: "10.87.0.1",
};

describe("decidePhase", () => {
  it("shows loading before the first answer", () => {
    expect(decidePhase(undefined, null)).toEqual({ kind: "loading" });
  });

  it("shows 'unreachable' when the device doesn't answer", () => {
    expect(decidePhase(undefined, "offline").kind).toBe("unreachable");
  });

  it("shows the setup wizard in setup mode", () => {
    const setupState = { ...base, mode: "setup" as const, claimed: false };
    expect(decidePhase({ setupState, loggedIn: false }, null)).toEqual({
      kind: "setup",
      setupState,
    });
  });

  it("shows a notice when setup.txt is broken on an unclaimed device", () => {
    const setupState = { ...base, mode: "setup-error" as const, claimed: false };
    expect(decidePhase({ setupState, loggedIn: false }, null)).toEqual({
      kind: "notice",
      mode: "setup-error",
    });
  });

  it("asks for login in normal mode", () => {
    expect(decidePhase({ setupState: base, loggedIn: false }, null)).toEqual({ kind: "login" });
  });

  it("shows the main pages when logged in", () => {
    expect(decidePhase({ setupState: base, loggedIn: true }, null)).toEqual({ kind: "main" });
  });
});
