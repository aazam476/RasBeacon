// Runs before each component test file: empties the page after every test, so tests don't see
// each other's screens.
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => cleanup());
