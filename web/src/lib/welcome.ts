import type { WelcomeSettings } from '@/data/types';

/** "How often to ask again" choices, in hours (0: until you ask again). */
export const ASK_AGAIN_OPTIONS: readonly { value: string; label: string }[] = [
  { value: '1', label: 'Every hour' },
  { value: '24', label: 'Every day' },
  { value: '168', label: 'Every week' },
  { value: '720', label: 'Every 30 days' },
  { value: '0', label: 'Only once' },
];

/** The largest logo, as a data: URL, and page the router keeps (internal/config). */
export const MAX_LOGO = 140_000;
export const MAX_PAGE = 64_000;

/** The problems the router finds in the welcome page's settings (config.WelcomeConfig.Validate). */
export function welcomeProblems(s: WelcomeSettings): Partial<Record<keyof WelcomeSettings, string>> {
  const problems: Partial<Record<keyof WelcomeSettings, string>> = {};
  const title = s.title.trim();
  if (title === '' || [...title].length > 60) problems.title = 'Give the page a title of up to 60 characters.';
  if ([...s.message].length > 1000) problems.message = 'Use at most 1000 characters.';
  if ([...s.terms].length > 10000) problems.terms = 'Use at most 10000 characters.';
  const code = s.accessCode.trim();
  if (code !== '' && ([...code].length < 4 || [...code].length > 32)) {
    problems.accessCode = 'Use 4 to 32 characters, or leave it empty for no code.';
  }
  if (s.color !== '' && !/^#[0-9a-f]{6}$/i.test(s.color)) problems.color = 'Choose a color like #0c8a9c.';
  if (s.logo !== '' && (s.logo.length > MAX_LOGO || !/^data:image\/(png|jpeg);base64,/.test(s.logo))) {
    problems.logo = 'Use a PNG or JPEG picture of up to 100 KB.';
  }
  if (s.customPage.length > MAX_PAGE) problems.customPage = 'Use a page of up to 64 KB.';
  return problems;
}
