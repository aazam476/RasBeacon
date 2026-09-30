import { describe, expect, it } from "vitest";
import { describeSignal, formatDuration } from "./format";

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
