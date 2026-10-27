// The welcome page's settings: the same checks as the router.
import { describe, expect, it } from 'vitest';
import type { WelcomeSettings } from '@/data/types';
import { welcomeProblems } from '@/lib/welcome';

const good: WelcomeSettings = {
  enabled: true,
  networks: 'both',
  title: 'Welcome',
  message: '',
  terms: '',
  accessCode: '',
  askAgainHours: 24,
  includeWired: false,
  color: '#0C8A9C',
  logo: 'data:image/jpeg;base64,/9j/4AAQ',
  customPage: '',
  accepted: 0,
};

describe('welcomeProblems', () => {
  it('finds nothing wrong with good settings', () => {
    expect(welcomeProblems(good)).toEqual({});
    expect(welcomeProblems({ ...good, color: '', logo: '', accessCode: '1234' })).toEqual({});
  });

  it.each([
    ['title', { title: '  ' }],
    ['title', { title: 'x'.repeat(61) }],
    ['message', { message: 'x'.repeat(1001) }],
    ['terms', { terms: 'x'.repeat(10001) }],
    ['accessCode', { accessCode: '123' }],
    ['accessCode', { accessCode: 'x'.repeat(33) }],
    ['color', { color: 'red' }],
    ['logo', { logo: 'data:image/svg+xml;base64,PHN2Zz4=' }],
    ['logo', { logo: `data:image/png;base64,${'A'.repeat(140_000)}` }],
    ['customPage', { customPage: 'x'.repeat(64_001) }],
  ] as const)('refuses a bad %s', (field, change) => {
    expect(Object.keys(welcomeProblems({ ...good, ...change }))).toEqual([field]);
  });
});
