import { describe, expect, it } from "vitest";
import type { SavedNetwork } from "../../api/types";
import {
  checkNewNetwork,
  EDUROAM_FORM,
  certificatesToUpload,
  fileToUpload,
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
      ...EMPTY_NEW_NETWORK,
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
      formFromNearby({
        ssid: "Cafe",
        signalDbm: -50,
        frequencyMhz: 2412,
        security: "open",
        label: "Open",
      }),
    ).toEqual({
      ...EMPTY_NEW_NETWORK,
      ssid: "Cafe",
      security: "open",
    });
  });
  it("opens the Enterprise form for eduroam", () => {
    const result = formFromNearby({
      ssid: "eduroam",
      signalDbm: -50,
      frequencyMhz: 5180,
      security: "enterprise",
      label: "WPA2-Enterprise",
    });
    expect(result).toMatchObject({ ssid: "eduroam", security: "enterprise", method: "ttls-pap" });
  });
  it("explains unsupported security", () => {
    const result = formFromNearby({
      ssid: "Old",
      signalDbm: -50,
      frequencyMhz: 2412,
      security: "other",
      label: "WEP (not supported)",
    });
    expect(result).toMatch(/WEP/);
  });
});

describe("networkErrorField", () => {
  it("splits server field names", () => {
    expect(networkErrorField("networks.2.password")).toEqual({ index: 2, input: "password" });
    expect(networkErrorField("networks.0.enterprise.caCert")).toEqual({
      index: 0,
      input: "enterprise.caCert",
    });
    expect(networkErrorField("ethernetRole")).toBeNull();
    expect(networkErrorField(undefined)).toBeNull();
  });
});

describe("Enterprise networks", () => {
  const ca = { name: "ca.pem", content: "-----BEGIN CERTIFICATE-----" };
  const complete = {
    ...EDUROAM_FORM,
    identity: "NetID@rutgers.edu",
    password: "pw",
    serverName: "*.ruw.rutgers.edu",
    caCert: ca,
  };

  it("accepts a complete eduroam form", () => {
    expect(checkNewNetwork(complete, [])).toEqual({});
  });
  it("requires the CA certificate and server name", () => {
    const errors = checkNewNetwork({ ...complete, caCert: null, serverName: "" }, []);
    expect(errors.caCert).toBeTruthy();
    expect(errors.serverName).toBeTruthy();
  });
  it("allows skipping the check only when turned off on purpose", () => {
    expect(
      checkNewNetwork(
        { ...complete, caCert: null, serverName: "", allowNoCertificateCheck: true },
        [],
      ),
    ).toEqual({});
  });
  it("rejects malformed server names", () => {
    expect(checkNewNetwork({ ...complete, serverName: "ruw rutgers" }, []).serverName).toBeTruthy();
  });
  it("needs certificate and key, not a password, for EAP-TLS", () => {
    const errors = checkNewNetwork({ ...complete, method: "tls", password: "" }, []);
    expect(errors.password).toBeUndefined();
    expect(errors.clientCert).toBeTruthy();
    expect(errors.clientKey).toBeTruthy();
  });
  it("builds the API entry", () => {
    const saved = toSavedNetwork(complete);
    expect(saved.enterprise).toMatchObject({
      method: "ttls-pap",
      identity: "NetID@rutgers.edu",
      serverName: "*.ruw.rutgers.edu",
      caCert: ca.content,
    });
    expect(saved.password).toBe("pw");
  });
});

describe("fileToUpload", () => {
  it("sends PEM files as text", () => {
    const pem = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n";
    expect(fileToUpload("ca.pem", new TextEncoder().encode(pem))).toEqual({
      name: "ca.pem",
      content: pem,
    });
  });
  it("sends binary files as base64", () => {
    expect(fileToUpload("ca.der", new Uint8Array([0x30, 0x82, 0xff]))).toEqual({
      name: "ca.der",
      content: "MIL/",
    });
  });
});

describe("certificatesToUpload", () => {
  it("combines PEM and binary certificates into one PEM bundle", () => {
    const pem = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n";
    const result = certificatesToUpload([
      { name: "usertrust.pem", bytes: new TextEncoder().encode(pem) },
      { name: "device-root.der", bytes: new Uint8Array([0x30, 0x82, 0xff]) },
    ]);
    expect(result.name).toBe("usertrust.pem, device-root.der");
    expect(result.content).toBe(
      pem + "-----BEGIN CERTIFICATE-----\nMIL/\n-----END CERTIFICATE-----\n",
    );
  });
});
