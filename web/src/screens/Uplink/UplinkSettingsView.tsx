// Markup for the Internet page (uplink settings).

import type { UplinkSettingsLogic } from "./useUplinkSettings";
import type { EAPMethod, EthernetRole, UplinkSecurity } from "../../api/types";
import { FieldError } from "../../shared/FieldError";
import { describeSignal, describeBand } from "../../shared/format";

export type UplinkSettingsViewProps = UplinkSettingsLogic;

export function UplinkSettingsView(props: UplinkSettingsViewProps) {
  const { values } = props;
  if (!values) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>Internet</h2>
      <p>
        RasBeacon uses an Ethernet cable when one is plugged in, otherwise the first network in your
        list that is in range.
      </p>

      {values.ethernetPresent && (
        <fieldset>
          <legend>Ethernet port</legend>
          <label>
            <input
              type="radio"
              name="ethernetRole"
              checked={values.ethernetRole === "uplink"}
              onChange={() => props.setEthernetRole("uplink" as EthernetRole)}
            />
            Uplink: the cable connects RasBeacon to the internet (for example a hotel room&apos;s
            wall socket)
          </label>
          <label>
            <input
              type="radio"
              name="ethernetRole"
              checked={values.ethernetRole === "client-port"}
              onChange={() => props.setEthernetRole("client-port" as EthernetRole)}
            />
            Client port: plug in one of your own devices (for example a laptop) to use the hotspot
            by cable
          </label>
        </fieldset>
      )}

      <h3>Wi-Fi networks to join</h3>
      {!values.wifiUplinkAvailable && (
        <p role="alert">
          No Wi-Fi radio is free to join a network: the only radio runs the hotspot. Plug in a USB
          Wi-Fi adapter for the hotspot, or use Ethernet.
        </p>
      )}
      {values.networks.length === 0 ? (
        <p>No networks saved yet.</p>
      ) : (
        <ol>
          {values.networks.map((network, index) => (
            <li key={network.id || `new-${network.ssid}`}>
              <strong>{network.ssid}</strong> {SECURITY_NAMES[network.security]}
              {network.hidden && " (hidden)"}
              {network.id === "" && " (not saved yet)"}{" "}
              <button
                type="button"
                onClick={() => props.moveUp(index)}
                disabled={index === 0}
                aria-label={`Move ${network.ssid} up`}
              >
                Up
              </button>
              <button
                type="button"
                onClick={() => props.moveDown(index)}
                disabled={index === values.networks.length - 1}
                aria-label={`Move ${network.ssid} down`}
              >
                Down
              </button>
              <button
                type="button"
                onClick={() => props.remove(index)}
                aria-label={`Remove ${network.ssid}`}
              >
                Remove
              </button>
              {network.enterprise && (
                <p>
                  Username: {network.enterprise.identity}. Login method:{" "}
                  {METHOD_NAMES[network.enterprise.method]}.{" "}
                  {network.enterprise.allowNoCertificateCheck ? (
                    <strong>Login server is NOT checked.</strong>
                  ) : (
                    <>
                      Server: {network.enterprise.serverName}. CA certificate:{" "}
                      {network.enterprise.caCertSummary || "none"}.
                    </>
                  )}
                </p>
              )}
              {network.security !== "open" && network.id !== "" && (
                <label>
                  New password for {network.ssid} (leave empty to keep it)
                  <input
                    type="text"
                    autoComplete="off"
                    value={network.password ?? ""}
                    onChange={(e) => props.setPassword(index, e.target.value)}
                  />
                </label>
              )}
              <FieldError message={props.networkErrors[index]} />
            </li>
          ))}
        </ol>
      )}

      <h3>Add a network</h3>
      <button
        type="button"
        onClick={props.scan}
        disabled={props.scanning || !values.wifiUplinkAvailable}
      >
        {props.scanning ? "Looking for networks…" : "Look for nearby networks"}
      </button>
      {props.scanError && <p role="alert">{props.scanError}</p>}
      {props.nearby !== null &&
        (props.nearby.length === 0 ? (
          <p>No networks found.</p>
        ) : (
          <ul>
            {props.nearby.map((network) => (
              <li key={network.ssid}>
                <button type="button" onClick={() => props.pickNearby(network)}>
                  {network.ssid}
                </button>{" "}
                {network.label}, {describeBand(network.frequencyMhz)},{" "}
                {describeSignal(network.signalDbm)}
              </li>
            ))}
          </ul>
        ))}

      <button type="button" onClick={props.applyEduroamPreset}>
        Set up eduroam
      </button>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.addNetwork();
        }}
      >
        <label>
          Network name
          <input
            type="text"
            value={props.newNetwork.ssid}
            onChange={(e) => props.setNewNetworkField("ssid", e.target.value)}
          />
        </label>
        <FieldError message={props.newNetworkErrors.ssid} />

        <label>
          Security
          <select
            value={props.newNetwork.security}
            onChange={(e) => props.setNewNetworkField("security", e.target.value as UplinkSecurity)}
          >
            <option value="personal">Password (WPA2/WPA3-Personal)</option>
            <option value="enterprise">
              Username and password (WPA2/WPA3-Enterprise, e.g. eduroam)
            </option>
            <option value="open">Open (no password)</option>
          </select>
        </label>

        {props.newNetwork.security === "enterprise" && <EnterpriseFields {...props} />}

        {props.newNetwork.security === "personal" && (
          <>
            <label>
              Network password
              <input
                type="text"
                autoComplete="off"
                value={props.newNetwork.password}
                onChange={(e) => props.setNewNetworkField("password", e.target.value)}
              />
            </label>
            <FieldError message={props.newNetworkErrors.password} />
          </>
        )}

        <label>
          <input
            type="checkbox"
            checked={props.newNetwork.hidden}
            onChange={(e) => props.setNewNetworkField("hidden", e.target.checked)}
          />
          Hidden network (doesn&apos;t show up in scans)
        </label>

        <button type="submit">Add to list</button>
      </form>

      <h3>Save</h3>
      {props.dirty && <p>You have changes that are not saved yet.</p>}
      {props.saveError && <p role="alert">{props.saveError}</p>}
      {props.saved && <p>Saved. RasBeacon is joining the networks now; see the Status page.</p>}
      <button type="button" onClick={props.save} disabled={props.saving || !props.dirty}>
        {props.saving ? "Saving…" : "Save"}
      </button>
    </section>
  );
}

const SECURITY_NAMES: Record<UplinkSecurity, string> = {
  open: "(open)",
  personal: "(password)",
  enterprise: "(Enterprise)",
};

const METHOD_NAMES: Record<EAPMethod, string> = {
  "ttls-pap": "EAP-TTLS with PAP",
  "peap-mschapv2": "PEAP with MSCHAPv2",
  tls: "EAP-TLS (certificate)",
};

/** The extra inputs for WPA-Enterprise networks (eduroam and similar). */
function EnterpriseFields(props: UplinkSettingsViewProps) {
  const { newNetwork: form, setNewNetworkField: setField, newNetworkErrors: errors } = props;
  return (
    <fieldset>
      <legend>Enterprise login</legend>
      <p>
        Your university or company publishes these settings (for eduroam, search for your
        institution&apos;s eduroam setup page).
      </p>

      <label>
        Login method
        <select
          value={form.method}
          onChange={(e) => setField("method", e.target.value as EAPMethod)}
        >
          <option value="ttls-pap">{METHOD_NAMES["ttls-pap"]}</option>
          <option value="peap-mschapv2">{METHOD_NAMES["peap-mschapv2"]}</option>
          <option value="tls">{METHOD_NAMES.tls}</option>
        </select>
      </label>

      <label>
        Username
        <input
          type="text"
          autoComplete="off"
          autoCapitalize="off"
          placeholder="NetID@example.edu"
          value={form.identity}
          onChange={(e) => setField("identity", e.target.value)}
        />
      </label>
      <FieldError message={errors.identity} />

      <label>
        Anonymous username (optional)
        <input
          type="text"
          autoComplete="off"
          autoCapitalize="off"
          placeholder="anonymous@example.edu"
          value={form.anonymousIdentity}
          onChange={(e) => setField("anonymousIdentity", e.target.value)}
        />
      </label>

      {form.method === "tls" ? (
        <>
          <label>
            Your certificate file
            <input
              type="file"
              accept=".pem,.crt,.cer,.der"
              onChange={(e) =>
                props.setNewNetworkFiles("clientCert", Array.from(e.target.files ?? []))
              }
            />
          </label>
          <FieldError message={errors.clientCert} />
          <label>
            Your key file
            <input
              type="file"
              accept=".pem,.key,.der"
              onChange={(e) =>
                props.setNewNetworkFiles("clientKey", Array.from(e.target.files ?? []))
              }
            />
          </label>
          <FieldError message={errors.clientKey} />
          <label>
            Key passphrase (if the key has one)
            <input
              type="password"
              autoComplete="off"
              value={form.password}
              onChange={(e) => setField("password", e.target.value)}
            />
          </label>
        </>
      ) : (
        <>
          <label>
            Your password
            <input
              type="password"
              autoComplete="off"
              value={form.password}
              onChange={(e) => setField("password", e.target.value)}
            />
          </label>
          <FieldError message={errors.password} />
        </>
      )}

      <label>
        CA certificate file (choose several if your institution lists more than one)
        <input
          type="file"
          multiple
          accept=".pem,.crt,.cer,.der"
          onChange={(e) => props.setNewNetworkFiles("caCert", Array.from(e.target.files ?? []))}
        />
      </label>
      {form.caCert && <p>Chosen: {form.caCert.name}</p>}
      <FieldError message={errors.caCert} />

      <label>
        Login server name
        <input
          type="text"
          autoComplete="off"
          autoCapitalize="off"
          placeholder="*.example.edu"
          value={form.serverName}
          onChange={(e) => setField("serverName", e.target.value)}
        />
      </label>
      <FieldError message={errors.serverName} />

      <details>
        <summary>Advanced</summary>
        <label>
          <input
            type="checkbox"
            checked={form.allowNoCertificateCheck}
            onChange={(e) => setField("allowNoCertificateCheck", e.target.checked)}
          />
          I understand the risk: don&apos;t check the login server
        </label>
        {form.allowNoCertificateCheck && (
          <p role="alert">
            Without the check, anyone nearby can create a fake network with the same name and
            collect your password. Only use this to test, and change your password afterwards.
          </p>
        )}
      </details>
    </fieldset>
  );
}
