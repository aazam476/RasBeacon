// Shared by the request tests: a fake network that records what the web UI sends, and a table
// format, so each area's file is a list of "this call sends this request".
//
// To test a new API function, add one line to its area's table:
//
//   { name: 'saves the allow list', call: () => api.putAccessSettings(settings),
//     method: 'PUT', path: '/settings/access', body: settings },

import { readFileSync } from 'node:fs';
import { expect, it, vi } from 'vitest';

/**
 * Every route docs/api.md lists, like "POST /api/v1/system/hostname", as patterns ({token} stands
 * for any value). rbweb's tests check the same list (internal/webserver/routes_test.go), so a
 * request that passes here is one the router serves.
 */
const DOCUMENTED_ROUTES: RegExp[] = [
  ...readFileSync(new URL('../../../docs/api.md', import.meta.url), 'utf8').matchAll(
    /`((?:GET|PUT|POST|DELETE) \/api\/v1\/[^`\s]*)`/g,
  ),
].map(([, route]) => new RegExp(`^${route.split('?')[0].replace(/\{[a-z]+\}/g, '[^/]+')}$`));

/** Whether docs/api.md lists this request (its query string left out). */
export function isDocumented(method: string, path: string): boolean {
  const route = `${method} /api/v1${path.split('?')[0]}`;
  return DOCUMENTED_ROUTES.some((pattern) => pattern.test(route));
}

/** What the web UI sent. A file upload's body is recorded as `'<file>'`. */
export interface SentRequest {
  method: string;
  path: string;
  body: unknown;
  headers: Record<string, string>;
}

/** One line of an area's table. */
export interface RequestCase {
  /** What the call does, in a few words; it names the test. */
  name: string;
  call: () => Promise<unknown>;
  method: 'GET' | 'POST' | 'PUT' | 'DELETE';
  /** The path after /api/v1. */
  path: string;
  /** The JSON body, or '<file>' for an upload; leave it out for requests without one. */
  body?: unknown;
}

/** How the fake router answers: 200 with `body` as JSON, or another status. */
export interface FakeAnswer {
  status?: number;
  body?: unknown;
}

/**
 * Replaces fetch with a fake router that answers every request with `answer`, and returns the
 * list it records each request in. Vitest puts the real fetch back after each test.
 */
export function fakeRouter(answer: FakeAnswer = {}): SentRequest[] {
  const sent: SentRequest[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const body = init.body;
      sent.push({
        method: init.method ?? 'GET',
        path: url.replace(/^\/api\/v1/, ''),
        body: body === undefined ? undefined : body instanceof Blob ? '<file>' : JSON.parse(String(body)),
        headers: { ...(init.headers as Record<string, string>) },
      });
      const status = answer.status ?? 200;
      return status === 204
        ? new Response(null, { status })
        : new Response(JSON.stringify(answer.body ?? {}), { status, headers: { 'Content-Type': 'application/json' } });
    }),
  );
  return sent;
}

/**
 * Makes one test per line of the table: the call sends exactly that request, and only one, and
 * the route is one docs/api.md lists.
 */
export function testRequests(cases: RequestCase[]) {
  it.each(cases)('$name: $method $path', async ({ call, method, path, body }) => {
    const sent = fakeRouter();
    await call();
    expect(sent).toHaveLength(1);
    expect({ method: sent[0].method, path: sent[0].path, body: sent[0].body }).toEqual({ method, path, body });
    expect(isDocumented(method, path), `${method} ${path} is not in docs/api.md`).toBe(true);
  });
}

/** A small file, for uploads. */
export function testFile(name = 'file.bin'): File {
  return new File(['data'], name, { type: 'application/octet-stream' });
}
