import { describe, expect, it } from "vitest";
import type { SavedNetwork } from "../../api/types";
import {
  checkNewNetwork,
  EMPTY_NEW_NETWORK,
  formFromNearby,
  moveNetwork,
  networkErrorField,
  removeNetwork,
  setNetworkPassword,
  toSavedNetwork,
} from "./networkList";

const home: SavedNetwork = {
  id: "a",
  ssid: "Home",
  security: "personal",
  hidden: false,
  hasPassword: true,
};
const hotel: SavedNetwork = {
  id: "b",
  ssid: "Hotel",
  security: "open",
  hidden: false,
  hasPassword: false,
};

describe("moveNetwork", () => {
  it("swaps with the neighbour", () => {
    expect(moveNetwork([home, hotel], 1, -1).map((n) => n.ssid)).toEqual(["Hotel", "Home"]);
  });
  it("does nothing past either end", () => {
    const list = [home, hotel];
    expect(moveNetwork(list, 0, -1)).toBe(list);
    expect(moveNetwork(list, 1, 1)).toBe(list);
  });
});

describe("removeNetwork and setNetworkPassword", () => {
  it("removes by position", () => {
    expect(removeNetwork([home, hotel], 0)).toEqual([hotel]);
  });
  it("sets a password without changing the original list", () => {
    const list = [home, hotel];
    const next = setNetworkPassword(list, 0, "new-password");
    expect(next[0].password).toBe("new-password");
    expect(list[0].password).toBeUndefined();
  });
});

describe("checkNewNetwork", () => {
  it("accepts a valid personal network", () => {
    expect(
      checkNewNetwork({ ...EMPTY_NEW_NETWORK, ssid: "Cafe", password: "espresso-1" }, [home]),
    ).toEqual({});
  });
  it("needs a password only for personal networks", () => {
    expect(checkNewNetwork({ ...EMPTY_NEW_NETWORK, ssid: "Cafe" }, []).password).toBeTruthy();
    expect(checkNewNetwork({ ...EMPTY_NEW_NETWORK, ssid: "Cafe", security: "open" }, [])).toEqual(
      {},
    );
  });
  it("rejects duplicates and empty names", () => {
    expect(
      checkNewNetwork({ ...EMPTY_NEW_NETWORK, ssid: "Home", security: "open" }, [home]).ssid,
    ).toMatch(/already/);
    expect(checkNewNetwork({ ...EMPTY_NEW_NETWORK, security: "open" }, []).ssid).toBeTruthy();
  });
});

describe("toSavedNetwork", () => {
  it("drops the password of open networks", () => {
    const saved = toSavedNetwork({
      ssid: "x",
      security: "open",
      password: "leftover",
      hidden: true,
    });
    expect(saved).toEqual({
      id: "",
      ssid: "x",
      security: "open",
      hidden: true,
      hasPassword: false,
      password: "",
    });
  });
});

describe("formFromNearby", () => {
  it("fills in name and security", () => {
    expect(
      formFromNearby({ ssid: "Cafe", signalDbm: -50, frequencyMhz: 2412, security: "open" }),
    ).toEqual({
      ...EMPTY_NEW_NETWORK,
      ssid: "Cafe",
      security: "open",
    });
  });
  it("explains enterprise networks are not supported yet", () => {
    const result = formFromNearby({
      ssid: "eduroam",
      signalDbm: -50,
      frequencyMhz: 5180,
      security: "enterprise",
    });
    expect(typeof result).toBe("string");
  });
});

describe("networkErrorField", () => {
  it("splits server field names", () => {
    expect(networkErrorField("networks.2.password")).toEqual({ index: 2, input: "password" });
    expect(networkErrorField("ethernetRole")).toBeNull();
    expect(networkErrorField(undefined)).toBeNull();
  });
});
