// Markup for the status page.

import type { StatusLogic } from "./useStatus";
import { describeSignal, describeUplink, formatDuration } from "../../shared/format";

export type StatusViewProps = StatusLogic;

export function StatusView({ status, error, loading, reload }: StatusViewProps) {
  if (!status) {
    return <section>{error ? <p role="alert">{error}</p> : <p>Loading…</p>}</section>;
  }
  const { hotspot, uplink } = status;

  return (
    <section>
      <h2>Status</h2>
      {error && <p role="alert">Last refresh failed: {error}</p>}

      {status.warnings.length > 0 && (
        <ul>
          {status.warnings.map((warning) => (
            <li key={warning} role="alert">
              {warning}
            </li>
          ))}
        </ul>
      )}

      <h3>Internet</h3>
      <dl>
        <dt>Connection</dt>
        <dd>{describeUplink(uplink)}</dd>
        {uplink.connected && (
          <>
            <dt>Address on that network</dt>
            <dd>{uplink.address || "waiting for address"}</dd>
            <dt>IPv6 for your devices</dt>
            <dd>{uplink.ipv6 ? "Yes" : "No (IPv4 only)"}</dd>
          </>
        )}
        {uplink.wifi.available && (
          <>
            <dt>Wi-Fi</dt>
            <dd>{describeWiFiState(uplink.wifi.state)}</dd>
            {uplink.wifi.state === "connected" && (
              <>
                <dt>Wi-Fi signal</dt>
                <dd>
                  {describeSignal(uplink.wifi.signalDbm)}, {uplink.wifi.band}
                </dd>
              </>
            )}
          </>
        )}
        {uplink.ethernet.present && (
          <>
            <dt>Ethernet</dt>
            <dd>
              {uplink.ethernet.role === "uplink" ? "Uplink" : "Client port"},{" "}
              {uplink.ethernet.cablePlugged ? "cable plugged in" : "no cable"}
            </dd>
          </>
        )}
      </dl>

      <h3>Hotspot</h3>
      <dl>
        <dt>Network name</dt>
        <dd>{hotspot.ssid}</dd>
        <dt>State</dt>
        <dd>{hotspot.running ? "Running" : "Not running"}</dd>
        <dt>Band and channel</dt>
        <dd>
          {hotspot.band}, channel {hotspot.channel}
        </dd>
        <dt>Security</dt>
        <dd>{hotspot.security === "wpa2-wpa3" ? "WPA2/WPA3" : "WPA2 only"}</dd>
        <dt>Address</dt>
        <dd>{hotspot.lanAddress}</dd>
      </dl>

      <h3>Connected devices ({status.clients.length})</h3>
      {status.clients.length === 0 ? (
        <p>No devices connected.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Address</th>
              <th>Signal</th>
              <th>Connected for</th>
            </tr>
          </thead>
          <tbody>
            {status.clients.map((client) => (
              <tr key={client.mac}>
                <td>{client.hostname || client.mac}</td>
                <td>{client.ip || "waiting for address"}</td>
                <td>{describeSignal(client.signalDbm)}</td>
                <td>{formatDuration(client.connectedSeconds)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h3>Wi-Fi radios</h3>
      <table>
        <thead>
          <tr>
            <th>Interface</th>
            <th>Role</th>
            <th>Driver</th>
            <th>USB ID</th>
          </tr>
        </thead>
        <tbody>
          {status.radios.map((radio) => (
            <tr key={radio.interface}>
              <td>{radio.interface}</td>
              <td>{radio.role}</td>
              <td>{radio.driver}</td>
              <td>{radio.isUsb ? radio.usbId : "built-in"}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3>Device</h3>
      <dl>
        <dt>Board</dt>
        <dd>{status.boardModel}</dd>
        <dt>RasBeacon version</dt>
        <dd>{status.version}</dd>
        <dt>Device ID</dt>
        <dd>{status.deviceId}</dd>
        <dt>Running for</dt>
        <dd>{formatDuration(status.uptimeSeconds)}</dd>
      </dl>

      <button type="button" onClick={reload} disabled={loading}>
        Refresh
      </button>
    </section>
  );
}

/** Wi-Fi uplink states in words. */
function describeWiFiState(state: string): string {
  switch (state) {
    case "connected":
      return "Connected";
    case "connecting":
      return "Connecting…";
    case "searching":
      return "Looking for your saved networks";
    case "not-configured":
      return "No networks saved (add one on the Internet page)";
    default:
      return "Off";
  }
}
