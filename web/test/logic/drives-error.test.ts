// When the USB drives can't be listed: what the drive field says.
import { describe, expect, it } from 'vitest';
import { ApiError } from '@/data/real/api';
import { drivesErrorText } from '@/screens/plugins/pluginDisplay';

describe('when the USB drives can’t be listed', () => {
  it('blames a slow drive, not the connection, when no answer came', () => {
    expect(drivesErrorText(new ApiError(0, "Can't reach RasBeacon."))).toMatch(/USB drives took too long/);
    expect(drivesErrorText(new ApiError(400, 'Listing drives: lsblk failed.'))).toBe('Listing drives: lsblk failed.');
  });
});
