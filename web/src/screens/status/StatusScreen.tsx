import { useDevice } from '@/data/device';
import { Loaded } from '@/shell/Loaded';
import { buttonClasses, Notice, NoticeList } from '@/ui';
import { Link } from 'react-router';
import { describeWiFiState } from '../common/describe';
import { PortalNotice } from '../common/PortalNotice';
import { HardwareSection } from './HardwareSection';
import { InternetSection } from './InternetSection';
import { DevicesSection, GuestSection, HotspotSection } from './NetworkSections';
import { StatusHero } from './StatusHero';
import { TrafficSection } from './TrafficSection';
import { VpnSection } from './VpnSection';

/**
 * The home screen: whether your devices are protected, then anything that needs attention,
 * then a group for each part of RasBeacon. On wide computer screens the groups flow into two
 * columns.
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
          {status.warnings.length > 0 && <NoticeList tone="warning" ariaLabel="Warnings" messages={status.warnings} />}
          <div className="flex flex-col gap-7 xl:block xl:columns-2 xl:gap-7 xl:*:mb-7 xl:*:break-inside-avoid">
            <VpnSection vpn={status.vpn} />
            <InternetSection status={status} />
            <TrafficSection throughput={status.throughput} />
            <DevicesSection clients={status.clients} guests={status.guest.clients.length} />
            <HotspotSection hotspot={status.hotspot} />
            <GuestSection guest={status.guest} />
            <HardwareSection status={status} />
          </div>
        </>
      )}
    </Loaded>
  );
}
