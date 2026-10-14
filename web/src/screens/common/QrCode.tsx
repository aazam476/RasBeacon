import type { WiFiQR } from '@/data/types';

/**
 * A QR code to join a Wi-Fi network, drawn from the router's SVG. It stays black on white in dark
 * mode too (cameras read it best that way), with the network's name under it.
 */
export function QrCode({ qr }: { qr: WiFiQR }) {
  // As an image, the SVG can't run anything.
  const src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(qr.svg)}`;
  return (
    <figure className="flex flex-col items-center gap-2 py-2">
      <img
        src={src}
        alt={`QR code to join ${qr.ssid}`}
        className="size-56 rounded-xl bg-white [image-rendering:pixelated]"
        draggable={false}
      />
      <figcaption className="text-subhead wrap-anywhere text-label-2">{qr.ssid}</figcaption>
    </figure>
  );
}
