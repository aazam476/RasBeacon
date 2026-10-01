import { describe, expect, it } from "vitest";
import { classifyGesture, pageSizeFor, toPagePoint } from "./browserGestures";

// The picture of a 390x700 page, shown at half size at (10, 100).
const box = { left: 10, top: 100, width: 195, height: 350 };
const page = { width: 390, height: 700 };

describe("toPagePoint", () => {
  it("scales a spot on the picture to the page", () => {
    expect(toPagePoint({ clientX: 60, clientY: 150 }, box, page)).toEqual({ x: 100, y: 100 });
  });
  it("keeps spots inside the page", () => {
    expect(toPagePoint({ clientX: 0, clientY: 9999 }, box, page)).toEqual({ x: 0, y: 700 });
  });
});

describe("classifyGesture", () => {
  it("treats a small movement as a tap", () => {
    expect(
      classifyGesture({ clientX: 60, clientY: 150 }, { clientX: 63, clientY: 154 }, box, page),
    ).toEqual({
      kind: "click",
      x: 100,
      y: 100,
    });
  });
  it("treats swiping up as scrolling down, scaled to the page", () => {
    expect(
      classifyGesture({ clientX: 60, clientY: 300 }, { clientX: 60, clientY: 200 }, box, page),
    ).toEqual({
      kind: "scroll",
      x: 100,
      y: 400,
      deltaY: 200,
    });
  });
});

describe("pageSizeFor", () => {
  it("leaves room for the controls", () => {
    expect(pageSizeFor(390, 844)).toEqual({ width: 390, height: 591 });
  });
});
