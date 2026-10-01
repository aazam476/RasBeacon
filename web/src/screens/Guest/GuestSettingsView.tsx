// Markup for the guest network page.

import type { GuestSettingsLogic } from "./useGuestSettings";
import { FieldError } from "../../shared/FieldError";

export type GuestSettingsViewProps = GuestSettingsLogic;

export function GuestSettingsView(props: GuestSettingsViewProps) {
  const { values, setField, errors } = props;
  if (!values) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>Guest network</h2>
      <p>
        A second Wi-Fi network for visitors, with its own password. Guests can use the internet
        (through the VPN, if it is on), but can&apos;t reach your devices, each other, or these
        settings.
      </p>
      {!values.supported && (
        <p role="alert">
          This Wi-Fi adapter can only broadcast one network, so it can&apos;t run a guest network.
          An adapter with a MediaTek chip (for example MT7612U) can.
        </p>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.save();
        }}
      >
        <label>
          <input
            type="checkbox"
            checked={values.enabled}
            disabled={!values.supported && !values.enabled}
            onChange={(e) => setField("enabled", e.target.checked)}
          />
          Turn on the guest network
        </label>
        <FieldError message={errors.enabled} />

        <label>
          Guest network name
          <input
            type="text"
            value={values.ssid}
            onChange={(e) => setField("ssid", e.target.value)}
          />
        </label>
        <FieldError message={errors.ssid} />

        <label>
          {values.hasPassword
            ? "New guest password (leave empty to keep the current one)"
            : "Guest password"}
          <input
            type="text"
            autoComplete="off"
            value={values.password ?? ""}
            onChange={(e) => setField("password", e.target.value)}
          />
        </label>
        <FieldError message={errors.password} />

        <p>Saving restarts the hotspot: your devices disconnect for a few seconds.</p>
        {props.saveError && <p role="alert">{props.saveError}</p>}
        {props.saved && <p>Saved. The hotspot has restarted.</p>}
        <button type="submit" disabled={props.saving}>
          {props.saving ? "Saving…" : "Save"}
        </button>
      </form>
    </section>
  );
}
