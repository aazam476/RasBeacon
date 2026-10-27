// The hotspot's band, security and standard choices, and how standards are named.
import { describe, expect, it } from 'vitest';
import {
  bandOptions,
  bestStandard,
  securityOptions,
  standardLabel,
  standardOptions,
} from '@/screens/common/wifiOptions';

const values = (options: { value: string }[]) => options.map((option) => option.value);

describe('Wi-Fi options', () => {
  it('offers the bands the radio and country allow', () => {
    expect(values(bandOptions(false))).toEqual(['2.4GHz', '5GHz']);
    expect(values(bandOptions(true))).toEqual(['2.4GHz']);
    expect(values(bandOptions(false, true))).toEqual(['2.4GHz', '5GHz', '6GHz']);
  });

  it('makes 6 GHz WPA3 only and Wi-Fi 6E', () => {
    expect(values(securityOptions(false, true))).toEqual(['wpa3']);
    expect(values(securityOptions(true))).toEqual(['wpa2-wpa3', 'wpa2']);
    expect(standardOptions('6GHz', true)).toEqual([{ value: 'wifi6', label: 'Wi-Fi 6E' }]);
    expect(bestStandard('6GHz', true)).toBe('wifi6');
    expect(values(standardOptions('5GHz', true))).toEqual(['wifi5', 'wifi4']);
  });

  it('names each standard, on one line', () => {
    const labels = ['wifi7', 'wifi6e', 'wifi6', 'wifi5', 'wifi4'].map((s) => standardLabel(s as never));
    expect(labels.map((label) => label.replace(/\u00a0/g, ' '))).toEqual([
      'Wi-Fi 7',
      'Wi-Fi 6E',
      'Wi-Fi 6',
      'Wi-Fi 5',
      'Wi-Fi 4',
    ]);
    expect(labels.every((label) => !label.includes(' '))).toBe(true);
  });
});
