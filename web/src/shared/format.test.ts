import { describe, expect, it } from "vitest";
import { describeBand, describeSignal, describeUplink, formatDuration } from "./format";

describe("formatDuration", () => {
  it.each([
    [42, "42 s"],
    [120, "2 min"],
    [3725, "1 h 2 min"],
    [90000, "1 d 1 h"],
  ])("%i seconds is %s", (seconds, text) => {
    expect(formatDuration(seconds)).toBe(text);
  });
});

describe("describeSignal", () => {
  it("rates signal strength", () => {
    expect(describeSignal(-48)).toBe("excellent (-48 dBm)");
    expect(describeSignal(-70)).toBe("fair (-70 dBm)");
    expect(describeSignal(-85)).toBe("poor (-85 dBm)");
    expect(describeSignal(0)).toBe("unknown");
  });
});

describe("describeBand", () => {
  it("names the band", () => {
    expect(describeBand(2437)).toBe("2.4 GHz");
    expect(describeBand(5180)).toBe("5 GHz");
    expect(describeBand(5955)).toBe("6 GHz");
    expect(describeBand(0)).toBe("unknown band");
  });
});

describe("describeUplink", () => {
  it("summarises the connection", () => {
    expect(describeUplink({ connected: false, kind: "", wifi: { ssid: "" } })).toBe(
      "Not connected",
    );
    expect(describeUplink({ connected: true, kind: "ethernet", wifi: { ssid: "x" } })).toBe(
      "Connected by Ethernet",
    );
    expect(describeUplink({ connected: true, kind: "wifi", wifi: { ssid: "Cafe" } })).toBe(
      'Connected to Wi-Fi "Cafe"',
    );
  });
});
