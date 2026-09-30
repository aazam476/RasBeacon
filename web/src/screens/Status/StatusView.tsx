// Markup for the status page.

import type { StatusLogic } from "./useStatus";
import { describeSignal, formatDuration } from "../../shared/format";

export type StatusViewProps = StatusLogic;

export function StatusView({ status, error, loading, reload }: StatusViewProps) {
  if (!status) {
    return <section>{error ? <p role="alert">{error}</p> : <p>Loading…</p>}</section>;
  }
  const { hotspot } = status;

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
