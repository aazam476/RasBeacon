// Markup for the VPN page.

import type { VPNSettingsLogic } from "./useVPNSettings";
import type { VPNForm } from "./vpnForm";

export type VPNSettingsViewProps = VPNSettingsLogic;

export function VPNSettingsView(props: VPNSettingsViewProps) {
  const { form, saved } = props;
  if (!form || !saved) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>VPN</h2>
      <p>
        RasBeacon can send all of your devices&apos; traffic through your own WireGuard VPN, so the
        network you&apos;re on (hotel, café, campus) can&apos;t see what you do.
      </p>
      {props.message && <p>{props.message}</p>}

      <h3>Import a WireGuard file</h3>
      <p>Your VPN provider or WireGuard server gives you a .conf file.</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.importConf();
        }}
      >
        <label>
          WireGuard file
          <input
            type="file"
            accept=".conf,text/plain"
            onChange={(e) => props.setImportFile(e.target.files?.[0] ?? null)}
          />
        </label>
        <label>
          Or paste its contents
          <textarea
            rows={8}
            spellCheck={false}
            value={props.importText}
            onChange={(e) => props.setImportText(e.target.value)}
          />
        </label>
        {props.importError && <p role="alert">{props.importError}</p>}
        <button type="submit" disabled={props.importing || props.importText.trim() === ""}>
          {props.importing ? "Importing…" : "Import"}
        </button>
      </form>

      <h3>Settings</h3>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.save();
        }}
      >
        <label>
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => props.setField("enabled", e.target.checked)}
          />
          Use the VPN
        </label>
        {!saved.configured && <p>Import a WireGuard file first.</p>}

        <label>
          <input
            type="checkbox"
            checked={form.killSwitch}
            onChange={(e) => props.setField("killSwitch", e.target.checked)}
          />
          Kill switch (recommended)
        </label>
        <p>
          On: if the VPN stops working, your devices have no internet until it works again, so
          nothing is ever sent without the VPN. Off: after 30 seconds without the VPN, devices use
          the internet directly.
        </p>

        <details>
          <summary>Server details (advanced)</summary>
          <TextField {...props} field="endpoint" label="Server (host:port)" />
          <TextField {...props} field="peerPublicKey" label="Server's public key" />
          <TextField {...props} field="addresses" label="RasBeacon's addresses in the tunnel" />
          <TextField {...props} field="dns" label="DNS servers (through the tunnel)" />
          <TextField {...props} field="allowedIps" label="Allowed IPs (must include 0.0.0.0/0)" />
          <TextField
            {...props}
            field="persistentKeepalive"
            label="Keepalive in seconds (empty = 25)"
          />
          <TextField {...props} field="mtu" label="MTU (empty = 1380)" />
          <TextField
            {...props}
            field="routedIpv6Prefix"
            label="Public IPv6 network your server routes to RasBeacon (optional, a /64)"
          />
          <p>
            Only if your WireGuard server routes a public IPv6 /64 to RasBeacon (for example
            2001:db8:1234:5678::/64): devices then get their own public IPv6 addresses through the
            VPN, without NAT. Leave it empty otherwise.
          </p>
          <TextField
            {...props}
            field="presharedKey"
            label={`New preshared key (optional${saved.hasPresharedKey ? "; one is saved" : ""})`}
          />
          <TextField {...props} field="privateKey" label="New private key (empty = keep)" />
          <p>
            RasBeacon&apos;s public key (give this to your WireGuard server):{" "}
            <code>{saved.publicKey || "none yet"}</code>
          </p>
          <button type="button" onClick={props.generateKey}>
            Make a new key pair
          </button>
        </details>

        {props.saveError && <p role="alert">{props.saveError}</p>}
        <button type="submit" disabled={props.saving}>
          {props.saving ? "Saving…" : "Save"}
        </button>
      </form>
    </section>
  );
}

/** One text input of the server details. */
function TextField(props: VPNSettingsViewProps & { field: keyof VPNForm; label: string }) {
  const value = props.form?.[props.field];
  return (
    <label>
      {props.label}
      <input
        type="text"
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        value={typeof value === "string" ? value : ""}
        aria-invalid={props.errorField === props.field || undefined}
        onChange={(e) => props.setField(props.field, e.target.value)}
      />
    </label>
  );
}
