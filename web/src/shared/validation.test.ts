import { describe, expect, it } from "vitest";
import {
  checkAdminPassword,
  checkPasswordsMatch,
  checkSsid,
  checkWifiPassword,
} from "./validation";

describe("checkSsid", () => {
  it("accepts a normal name", () => expect(checkSsid("My Hotspot")).toBeNull());
  it("rejects an empty name", () => expect(checkSsid("")).not.toBeNull());
  it("counts bytes, not characters", () => {
    expect(checkSsid("a".repeat(32))).toBeNull();
    // 11 emoji × 4 bytes = 44 bytes: too long even though it's 11 characters.
    expect(checkSsid("☕".repeat(11))).not.toBeNull();
  });
  it("rejects a newline", () => expect(checkSsid("a\nb")).not.toBeNull());
});

describe("checkWifiPassword", () => {
  it("accepts 8 characters", () => expect(checkWifiPassword("12345678")).toBeNull());
  it("rejects 7 characters", () => expect(checkWifiPassword("1234567")).not.toBeNull());
  it("rejects 64 characters", () => expect(checkWifiPassword("x".repeat(64))).not.toBeNull());
  it("rejects accented letters", () => expect(checkWifiPassword("pässwörd123")).not.toBeNull());
  it("accepts spaces and punctuation", () => expect(checkWifiPassword("a b, c!d?e")).toBeNull());
});

describe("checkAdminPassword", () => {
  it("rejects 9 characters", () => expect(checkAdminPassword("123456789")).not.toBeNull());
  it("accepts 10 characters", () => expect(checkAdminPassword("abcdefghij")).toBeNull());
  it("rejects the device name", () => expect(checkAdminPassword("myRasBeacon1")).not.toBeNull());
});

describe("checkPasswordsMatch", () => {
  it("matches", () => expect(checkPasswordsMatch("a", "a")).toBeNull());
  it("differs", () => expect(checkPasswordsMatch("a", "b")).not.toBeNull());
});
