// Markup for the hotspot settings page.

import type { HotspotSettingsLogic } from "./useHotspotSettings";
import type { Band, Security } from "../../api/types";
import { FieldError } from "../../shared/FieldError";
import { CHANNELS } from "../../shared/validation";

export type HotspotSettingsViewProps = HotspotSettingsLogic;

export function HotspotSettingsView(props: HotspotSettingsViewProps) {
  const { values, setField, errors } = props;
  if (!values) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>Hotspot</h2>
      <p>Saving restarts the hotspot: connected devices disconnect for a few seconds.</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.save();
        }}
      >
        <label>
          Network name
          <input
            type="text"
            value={values.ssid}
            onChange={(e) => setField("ssid", e.target.value)}
          />
        </label>
        <FieldError message={errors.ssid} />

        <label>
          New Wi-Fi password (leave empty to keep the current one)
          <input
            type="text"
            autoComplete="off"
            value={values.password ?? ""}
            onChange={(e) => setField("password", e.target.value)}
          />
        </label>
        <FieldError message={errors.password} />

        <label>
          Band
          <select value={values.band} onChange={(e) => setField("band", e.target.value as Band)}>
            <option value="2.4GHz">2.4 GHz (works with every device, longer range)</option>
            <option value="5GHz">5 GHz (faster, shorter range)</option>
          </select>
        </label>

        <label>
          Channel
          <select
            value={values.channel}
            onChange={(e) => setField("channel", Number(e.target.value))}
          >
            <option value={0}>Default</option>
            {CHANNELS[values.band].map((channel) => (
              <option key={channel} value={channel}>
                {channel}
              </option>
            ))}
          </select>
        </label>
        <FieldError message={errors.channel} />

        <label>
          Security
          <select
            value={values.security}
            onChange={(e) => setField("security", e.target.value as Security)}
          >
            <option value="wpa2-wpa3">WPA2/WPA3 (recommended)</option>
            <option value="wpa2">WPA2 only (for old devices that can't join)</option>
          </select>
        </label>

        <label>
          Country (two-letter code, decides which channels are legal)
          <input
            type="text"
            maxLength={2}
            value={values.country}
            onChange={(e) => setField("country", e.target.value.toUpperCase())}
          />
        </label>
        <FieldError message={errors.country} />

        {props.saveError && <p role="alert">{props.saveError}</p>}
        {props.saved && <p>Saved. The hotspot has restarted.</p>}
        <button type="submit" disabled={props.saving}>
          {props.saving ? "Saving…" : "Save"}
        </button>
      </form>

      <h3>Show the current Wi-Fi password</h3>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.reveal();
        }}
      >
        <label>
          Admin password
          <input
            type="password"
            autoComplete="current-password"
            value={props.revealAdminPassword}
            onChange={(e) => props.setRevealAdminPassword(e.target.value)}
          />
        </label>
        {props.revealError && <p role="alert">{props.revealError}</p>}
        <button type="submit">Show password</button>
      </form>
      {props.revealedPassword !== null && (
        <p>
          Wi-Fi password: <code>{props.revealedPassword}</code>
        </p>
      )}
    </section>
  );
}
