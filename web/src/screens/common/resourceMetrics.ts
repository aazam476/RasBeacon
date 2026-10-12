import type { SystemStats } from '@/data/types';
import { NBSP, percentOf } from '@/lib/format';
import type { BaseIconName } from '@/ui';

export interface ResourceMetric {
  label: string;
  icon: BaseIconName;
  /** The reading: "24%", "382 of 1024 MB", "42 °C". */
  value: string;
  /** A note after it: the CPU model, "37% used", "Nominal". */
  detail: string;
  /** How full its meter is, 0–100. */
  percent: number;
  /** Orange or red when the reading runs high; swap is secondary, so gray. */
  tone: 'tint' | 'gray' | 'orange' | 'red';
}

/**
 * CPU, memory, swap and temperature formatted once, so the Status and System pages show the
 * same labels and numbers. Iterating the result gives that order.
 */
export function getResourceMetrics(stats: SystemStats): Record<'cpu' | 'ram' | 'swap' | 'thermal', ResourceMetric> {
  const ramPercent = percentOf(stats.ramUsedMb, stats.ramTotalMb);
  const swapPercent = percentOf(stats.swapUsedMb, stats.swapTotalMb);
  const temperature = Math.round(stats.temperatureC);
  // The Raspberry Pi slows itself down from 80 °C and more from 85 °C.
  const hot = temperature >= 85 ? 'red' : temperature >= 80 ? 'orange' : 'tint';

  return {
    cpu: {
      label: 'CPU load',
      icon: 'memory',
      value: `${Math.round(stats.cpuPercent)}%`,
      detail: stats.cpuModel,
      percent: stats.cpuPercent,
      tone: stats.cpuPercent >= 90 ? 'orange' : 'tint',
    },
    ram: {
      label: 'Memory',
      icon: 'developer_board',
      value: `${Math.round(stats.ramUsedMb)} of ${Math.round(stats.ramTotalMb)}${NBSP}MB`,
      detail: `${Math.round(ramPercent)}% used`,
      percent: ramPercent,
      tone: ramPercent >= 90 ? 'orange' : 'tint',
    },
    swap: {
      label: 'Swap',
      icon: 'swap_horiz',
      value: `${Math.round(stats.swapUsedMb)} of ${Math.round(stats.swapTotalMb)}${NBSP}MB`,
      detail: `${Math.round(swapPercent)}% used`,
      percent: swapPercent,
      tone: 'gray',
    },
    thermal: {
      label: 'Temperature',
      icon: 'device_thermostat',
      value: `${temperature}${NBSP}°C`,
      detail: stats.thermalState,
      // The meter shows temperature on a 0–100 °C scale.
      percent: stats.temperatureC,
      tone: hot,
    },
  };
}
