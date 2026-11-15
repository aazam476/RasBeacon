// Which form field the router's answer about a share belongs under.
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { ApiError } from '@/data/real/api';
import { screenError, shareFields } from '@/data/real/logic';

describe('field errors for shares', () => {
  it('knows a share’s fields, its settings included', () => {
    for (const field of ['name', 'drive', 'folder', 'quotaMb', 'share', 'settings.readOnly']) {
      const error = screenError(new ApiError(400, 'Problem.', field), shareFields);
      expect((error as FieldErrors).fields).toEqual({ [field]: 'Problem.' });
    }
    // Anything else stays a message of its own.
    const other = screenError(new ApiError(400, 'Something else.', 'id'), shareFields);
    expect(other).not.toBeInstanceOf(FieldErrors);
  });
});
