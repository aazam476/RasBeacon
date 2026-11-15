// A plugin's storage size refused by RasBeacon shows under the size field.
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { ApiError } from '@/data/real/api';
import { pluginFields, screenError } from '@/data/real/logic';

describe('a plugin’s storage size refused', () => {
  it('shows under the size field', () => {
    const fields = pluginFields({ ports: {}, settings: {}, drive: '' });
    const error = screenError(new ApiError(400, 'The SD card has room for at most 3 GB.', 'storageMb'), fields);
    expect(error).toBeInstanceOf(FieldErrors);
    expect((error as FieldErrors).fields).toEqual({ storageMb: 'The SD card has room for at most 3 GB.' });
  });
});
