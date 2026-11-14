// The size of a plugin's data as the form shows and sends it (1 GB = 1024 MB).
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { choicesOf, draftOf, storageGbText } from '@/screens/plugins/pluginDisplay';

describe('a plugin’s storage size', () => {
  it('shows MB as GB to a tenth, rounded the way asked', () => {
    expect(storageGbText(2048)).toBe('2');
    expect(storageGbText(1536)).toBe('1.5');
    expect(storageGbText(476, 'up')).toBe('0.5');
    expect(storageGbText(6200, 'down')).toBe('6');
    expect(storageGbText(6200)).toBe('6.1');
  });

  it('is sent only for a plugin that lets the owner choose it, on internal storage', () => {
    expect(choicesOf(draftOf([], [], '')).storageMb).toBeUndefined();
    expect(choicesOf(draftOf([], [], '', 2048)).storageMb).toBe(2048);
    expect(choicesOf({ ...draftOf([], [], '', 2048), storage: ' 0.5 ' }).storageMb).toBe(512);
    // With a USB drive, the files are on the drive: no size goes along.
    expect(choicesOf(draftOf([], [], '6A1F-22C9', 2048)).storageMb).toBeUndefined();
  });

  it('is required on internal storage', () => {
    expect(() => choicesOf({ ...draftOf([], [], '', 2048), storage: ' ' })).toThrow(FieldErrors);
    expect(() => choicesOf({ ...draftOf([], [], '', 2048), storage: '.' })).toThrow(FieldErrors);
    expect(() => choicesOf({ ...draftOf([], [], '', 2048), storage: '-1' })).toThrow(FieldErrors);
  });
});
