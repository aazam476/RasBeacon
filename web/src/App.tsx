import { HashRouter, Navigate, Route, Routes } from 'react-router';
import type { ReactNode } from 'react';
import { DemoDeviceProvider } from '@/data/demo/DemoDeviceProvider';
import { useDevice } from '@/data/device';
import { RealDeviceProvider } from '@/data/real/RealDeviceProvider';
import { DESKTOP_QUERY, useMediaQuery } from '@/lib/useMediaQuery';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { GuestsScreen } from '@/screens/guests/GuestsScreen';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { HttpsScreen } from '@/screens/https/HttpsScreen';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { PortForwardingScreen } from '@/screens/internet/PortForwardingScreen';
import { WelcomeScreen } from '@/screens/guests/WelcomeScreen';
import { LoginScreen } from '@/screens/phases/LoginScreen';
import { LoadingScreen, NoticeScreen } from '@/screens/phases/NoticeScreen';
import { SetupScreen } from '@/screens/phases/SetupScreen';
import { CatalogScreen } from '@/screens/plugins/CatalogScreen';
import { PluginScreen } from '@/screens/plugins/PluginScreen';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { PortalScreen } from '@/screens/portal/PortalScreen';
import { SshScreen } from '@/screens/ssh/SshScreen';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { DesktopLayout } from '@/shell/DesktopLayout';
import { MoreScreen } from '@/shell/MoreScreen';
import { PhoneLayout } from '@/shell/PhoneLayout';
import { ErrorToastProvider } from '@/ui';

/**
 * One app, two layouts of the same screens: phones and tablets get iOS's (a tab bar), and
 * screens 1024px and wider get macOS's (a sidebar). Both use the same URLs, so resizing the
 * window keeps you on the same page.
 *
 * Routes are hash-based (`/#/vpn`) so the app works when served as static files from the
 * router itself, without server-side rewrites.
 */
export function App() {
  return (
    <HashRouter>
      <ErrorToastProvider>
        <DeviceProvider>
          <Routes>
            {/* The hotel login page screen opens without logging in. */}
            <Route path="/portal" element={<PortalRoute />} />
            <Route path="*" element={<PhaseGate />} />
          </Routes>
        </DeviceProvider>
      </ErrorToastProvider>
    </HashRouter>
  );
}

/**
 * The router itself, or the demo router (`npm run demo`, for working on the UI without a router).
 * The mode is fixed when the app is built, so the router's build contains no demo.
 */
function DeviceProvider({ children }: { children: ReactNode }) {
  return import.meta.env.MODE === 'demo' ? (
    <DemoDeviceProvider>{children}</DemoDeviceProvider>
  ) : (
    <RealDeviceProvider>{children}</RealDeviceProvider>
  );
}

/** The login page screen, as soon as setup is done; before that, whatever the app shows. */
function PortalRoute() {
  const { phase } = useDevice();
  return phase.kind === 'login' || phase.kind === 'ready' ? <PortalScreen /> : <PhaseGate />;
}

/** Full-page screens until the router is set up and you are logged in; then the pages. */
function PhaseGate() {
  const { phase } = useDevice();

  switch (phase.kind) {
    case 'loading':
      return <LoadingScreen />;
    case 'unreachable':
      return <NoticeScreen kind="unreachable" detail={phase.error} />;
    case 'notice':
      return <NoticeScreen kind={phase.notice} />;
    case 'setup':
      return <SetupScreen />;
    case 'login':
      return <LoginScreen />;
    case 'ready':
      return <Pages />;
  }
}

/** The logged-in pages in the layout for this screen size (exported for the tests). */
export function Pages() {
  const Layout = useMediaQuery(DESKTOP_QUERY) ? DesktopLayout : PhoneLayout;

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<StatusScreen />} />
        <Route path="internet" element={<InternetScreen />} />
        <Route path="internet/port-forwarding" element={<PortForwardingScreen />} />
        <Route path="vpn" element={<VpnScreen />} />
        <Route path="hotspot" element={<HotspotScreen />} />
        <Route path="guests" element={<GuestsScreen />} />
        <Route path="guests/welcome" element={<WelcomeScreen />} />
        <Route path="clients" element={<ClientsScreen />} />
        <Route path="ssh" element={<SshScreen />} />
        <Route path="https" element={<HttpsScreen />} />
        <Route path="system" element={<SystemScreen />} />
        <Route path="plugins" element={<PluginsScreen />} />
        <Route path="plugins/catalog" element={<CatalogScreen />} />
        <Route path="plugins/:id" element={<PluginScreen />} />
        <Route path="more" element={<MoreScreen />} />
      </Route>
      {/* Addresses from before this UI (/#hotspot, /#portal, …) match the routes by themselves, except
          /#guest; anything unknown, /#status included, goes to Status. */}
      <Route path="guest" element={<Navigate to="/guests" replace />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
