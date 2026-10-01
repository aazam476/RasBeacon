import { describe, expect, it } from "vitest";
import type { GuestSettings } from "../../api/types";
import { checkGuestForm } from "./useGuestSettings";

const base: GuestSettings = {
  enabled: true,
  ssid: "Guests",
  password: "",
  hasPassword: false,
  supported: true,
};

describe("checkGuestForm", () => {
  it("needs a password the first time", () => {
    expect(checkGuestForm(base).password).toMatch(/password/);
    expect(checkGuestForm({ ...base, hasPassword: true })).toEqual({});
  });
  it("checks the name and password", () => {
    expect(checkGuestForm({ ...base, ssid: "", password: "short" })).toMatchObject({
      ssid: expect.any(String),
      password: expect.any(String),
    });
  });
  it("doesn't check anything when turning it off", () => {
    expect(checkGuestForm({ ...base, enabled: false, ssid: "" })).toEqual({});
  });
});
