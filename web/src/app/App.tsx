// The top of the web UI. It connects each screen's logic hook to its view.
//
// Pattern used everywhere:  <SomethingView {...useSomething()} />
// The hook (logic) and the view (markup) live in the same folder under
// src/screens/. A designer can replace any *View.tsx without touching the
// logic, as long as the props stay the same (see docs/ui-contract.md).

import { useApp, type Page } from "./useApp";
import { LayoutView } from "./LayoutView";
import { NoticeView } from "../screens/Notice/NoticeView";
import { SetupView } from "../screens/Setup/SetupView";
import { useSetup } from "../screens/Setup/useSetup";
import { LoginView } from "../screens/Login/LoginView";
import { useLogin } from "../screens/Login/useLogin";
import { StatusView } from "../screens/Status/StatusView";
import { useStatus } from "../screens/Status/useStatus";
import { HotspotSettingsView } from "../screens/HotspotSettings/HotspotSettingsView";
import { useHotspotSettings } from "../screens/HotspotSettings/useHotspotSettings";
import { UplinkSettingsView } from "../screens/Uplink/UplinkSettingsView";
import { useUplinkSettings } from "../screens/Uplink/useUplinkSettings";
import { SSHSettingsView } from "../screens/SSHSettings/SSHSettingsView";
import { useSSHSettings } from "../screens/SSHSettings/useSSHSettings";
import { SystemView } from "../screens/System/SystemView";
import { useSystem } from "../screens/System/useSystem";
import type { SetupState } from "../api/types";

export function App() {
  const app = useApp();
  const { phase } = app;

  switch (phase.kind) {
    case "loading":
      return <NoticeView kind="loading" />;
    case "unreachable":
      return <NoticeView kind="unreachable" detail={phase.error} retry={app.retry} />;
    case "notice":
      return <NoticeView kind={phase.mode} retry={app.retry} />;
    case "setup":
      return <SetupScreen state={phase.setupState} />;
    case "login":
      return <LoginScreen onLoggedIn={app.onLoggedIn} />;
    case "main":
      return (
        <LayoutView page={app.page} setPage={app.setPage}>
          <MainPage page={app.page} onLoggedOut={app.onLoggedOut} />
        </LayoutView>
      );
  }
}

// Each small component below calls one logic hook and hands the result to
// the matching view. (Hooks must be called inside a component, which is
// why these exist instead of calling the hooks in App directly.)

function SetupScreen({ state }: { state: SetupState }) {
  return <SetupView {...useSetup(state)} />;
}

function LoginScreen({ onLoggedIn }: { onLoggedIn: () => void }) {
  return <LoginView {...useLogin(onLoggedIn)} />;
}

function MainPage({ page, onLoggedOut }: { page: Page; onLoggedOut: () => void }) {
  switch (page) {
    case "status":
      return <StatusScreen />;
    case "internet":
      return <InternetScreen />;
    case "hotspot":
      return <HotspotScreen />;
    case "ssh":
      return <SSHScreen />;
    case "system":
      return <SystemScreen onLoggedOut={onLoggedOut} />;
  }
}

function StatusScreen() {
  return <StatusView {...useStatus()} />;
}

function InternetScreen() {
  return <UplinkSettingsView {...useUplinkSettings()} />;
}

function HotspotScreen() {
  return <HotspotSettingsView {...useHotspotSettings()} />;
}

function SSHScreen() {
  return <SSHSettingsView {...useSSHSettings()} />;
}

function SystemScreen({ onLoggedOut }: { onLoggedOut: () => void }) {
  return <SystemView {...useSystem(onLoggedOut)} />;
}
