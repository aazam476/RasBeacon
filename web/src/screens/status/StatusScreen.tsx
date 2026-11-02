import { useDevice } from '@/data/device';
import { Loaded } from '@/shell/Loaded';
import { buttonClasses, Notice, NoticeList } from '@/ui';
import { Link } from 'react-router';
import { describeWiFiState } from '../common/describe';
import { PortalNotice } from '../common/PortalNotice';
import { HardwareSection } from './HardwareSection';
import { InternetSection } from './InternetSection';
import { PluginsSection } from './PluginsSection';
import { DevicesSection, GuestSection, HotspotSection } from './NetworkSections';
import { RadioRolesNotice } from '../system/RadiosSection';
import { StatusHero } from './StatusHero';
import { TrafficSection } from './TrafficSection';
import { VpnSection } from './VpnSection';

/**
 * The home screen: whether your devices are protected, then anything that needs attention,
 * then a group for each part of RasBeacon. On wide computer screens the groups sit in two
 * columns, each one stacked under the other.
 */
export function StatusScreen() {
  const { status, refreshStatus } = useDevice();

  return (
    <Loaded data={status} what="the status" onRetry={refreshStatus}>
      {(status) => (
        <>
          {/* The answer first; then what needs doing about it, and anything else to look at. */}
          <StatusHero status={status} />
          {status.portal.detected && <PortalNotice portal={status.portal} />}
          {status.uplink.wifi.available && status.uplink.wifi.state === 'login-failed' && (
            <Notice
              tone="danger"
              role="alert"
              actions={
                <Link to="/internet" className={buttonClasses('secondary', 'sm')}>
                  Open Internet settings
                </Link>
              }
            >
              <p>{describeWiFiState('login-failed')}</p>
            </Notice>
          )}
          <RadioRolesNotice secondsLeft={status.radioRolesPending} />
          {status.warnings.length > 0 && <NoticeList tone="warning" ariaLabel="Warnings" messages={status.warnings} />}
          {/* Two columns on wide screens that start at the same height, each a stack of its own
              sections. On narrower screens the columns dissolve into one list, in this order. */}
          <div className="flex flex-col gap-7 xl:flex-row xl:items-start">
            <div className="contents xl:flex xl:min-w-0 xl:flex-1 xl:flex-col xl:gap-7">
              <div className="order-1 xl:order-none">
                <VpnSection vpn={status.vpn} />
              </div>
              <div className="order-3 xl:order-none">
                <TrafficSection throughput={status.throughput} />
              </div>
              <div className="order-5 xl:order-none">
                <HotspotSection hotspot={status.hotspot} />
              </div>
              {/* Without plugins there is no section, and so no empty gap. */}
              {status.plugins.length > 0 && (
                <div className="order-7 xl:order-none">
                  <PluginsSection plugins={status.plugins} />
                </div>
              )}
            </div>
            <div className="contents xl:flex xl:min-w-0 xl:flex-1 xl:flex-col xl:gap-7">
              <div className="order-2 xl:order-none">
                <InternetSection status={status} />
              </div>
              <div className="order-4 xl:order-none">
                <DevicesSection clients={status.clients} guests={status.guest.clients.length} />
              </div>
              <div className="order-6 xl:order-none">
                <GuestSection guest={status.guest} />
              </div>
              <div className="order-8 xl:order-none">
                <HardwareSection status={status} />
              </div>
            </div>
          </div>
        </>
      )}
    </Loaded>
  );
}
