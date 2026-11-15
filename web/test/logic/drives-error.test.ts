// When the USB drives can't be listed: what the drive field says.
import { describe, expect, it } from 'vitest';
import { ApiError } from '@/data/real/api';
import { drivesErrorText } from '@/screens/plugins/pluginDisplay';

describe('when the USB drives can’t be listed', () => {
  it('blames a slow drive when RasBeacon answered too late', () => {
    expect(drivesErrorText(new ApiError(0, "Can't reach RasBeacon.", undefined, true))).toMatch(
      /USB drives took too long/,
    );
  });

  it('says nothing when RasBeacon wasn’t reached (the note at the top of the page says so)', () => {
    expect(drivesErrorText(new ApiError(0, "Can't reach RasBeacon."))).toBeNull();
  });

  it('shows what RasBeacon said otherwise', () => {
    expect(drivesErrorText(new ApiError(400, 'Listing drives: lsblk failed.'))).toBe('Listing drives: lsblk failed.');
  });
});
