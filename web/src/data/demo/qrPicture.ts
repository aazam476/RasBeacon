/**
 * A picture shaped like a QR code for the demo: the three corner squares and a pattern made from
 * the text, so different networks look different. It can't be scanned; the router makes real ones.
 */
export function demoQrSvg(text: string): string {
  const size = 29;
  let seed = 2166136261;
  for (const char of text) seed = Math.imul(seed ^ char.charCodeAt(0), 16777619);
  const next = () => {
    seed ^= seed << 13;
    seed ^= seed >>> 17;
    seed ^= seed << 5;
    return (seed >>> 0) / 4294967296;
  };
  const inCorner = (x: number, y: number) => (x < 8 && y < 8) || (x >= size - 8 && y < 8) || (x < 8 && y >= size - 8);
  const cornerDark = (x: number, y: number) => {
    const cx = x >= size - 8 ? x - (size - 7) : x;
    const cy = y >= size - 8 ? y - (size - 7) : y;
    if (cx < 0 || cy < 0 || cx > 6 || cy > 6) return false;
    const ring = Math.min(cx, cy, 6 - cx, 6 - cy);
    return ring !== 1;
  };
  let path = '';
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      const dark = inCorner(x, y) ? cornerDark(x, y) : next() < 0.5;
      if (dark) path += `M${x + 4} ${y + 4}h1v1h-1z`;
    }
  }
  const box = size + 8;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${box} ${box}" shape-rendering="crispEdges"><rect width="${box}" height="${box}" fill="#fff"/><path d="${path}" fill="#000"/></svg>`;
}
