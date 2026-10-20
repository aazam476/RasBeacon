// The web UI's tests (`npm test`). They live in test/, one folder per kind,
// each run as its own project (see docs/testing/README.md):
//
//   test/logic/       pure functions: form checks, parsing, formatting
//   test/requests/    what each API call sends to the router
//   test/components/  screens rendered in a simulated browser (jsdom)
//
// `npm run coverage` also reports how much of src/ the tests run.
import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config.ts';

export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      restoreMocks: true,
      projects: [
        { extends: true, test: { name: 'logic', include: ['test/logic/**/*.test.ts'] } },
        { extends: true, test: { name: 'requests', include: ['test/requests/**/*.test.ts'] } },
        {
          extends: true,
          test: {
            name: 'components',
            include: ['test/components/**/*.test.tsx'],
            environment: 'jsdom',
            setupFiles: ['test/components/setup.ts'],
          },
        },
      ],
      coverage: {
        provider: 'v8',
        include: ['src/**/*.{ts,tsx}'],
        // The demo's sample data and the entry point aren't app logic.
        exclude: ['src/data/demo/**', 'src/main.tsx', 'src/**/*.d.ts'],
        reporter: ['text-summary', 'text'],
      },
    },
  }),
);
