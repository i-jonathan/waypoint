import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api } from '../src/lib/api';

const fetchMock = vi.fn<typeof fetch>();

function reply(status: number, body: unknown, contentType = 'application/json'): Response {
  const text = typeof body === 'string' ? body : JSON.stringify(body);
  return new Response(status === 204 ? null : text, { status, headers: { 'Content-Type': contentType } });
}

function lastCall(): { url: string; init: RequestInit; headers: Headers; body: unknown } {
  const [url, init] = fetchMock.mock.calls.at(-1)!;
  const headers = new Headers(init?.headers);
  const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
  return { url: String(url), init: init ?? {}, headers, body };
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('requests', () => {
  it('sends X-Waypoint: 1 on reads and writes', async () => {
    fetchMock.mockImplementation(async () => reply(200, []));
    await api.projects();
    expect(lastCall().headers.get('X-Waypoint')).toBe('1');
    expect(lastCall().init.method ?? 'GET').toBe('GET');

    fetchMock.mockImplementation(async () => reply(204, ''));
    await api.deleteCard('P', 'P-1');
    expect(lastCall().headers.get('X-Waypoint')).toBe('1');
    expect(lastCall().init.method).toBe('DELETE');
  });

  it('encodes project names containing slashes, spaces and percent signs', async () => {
    fetchMock.mockImplementation(async () => reply(200, { board: {}, appearance: {}, readonly: false }));
    await api.board('a/b c%d');
    expect(lastCall().url).toBe('/api/projects/a%2Fb%20c%25d/board');
  });

  it('sends the filter query for the board and the archive', async () => {
    fetchMock.mockImplementation(async () => reply(200, { board: {}, appearance: {}, readonly: false }));
    await api.board('P', 'group:"Q&A" #1');
    expect(lastCall().url).toBe('/api/projects/P/board?q=group%3A%22Q%26A%22+%231');
    fetchMock.mockImplementation(async () => reply(200, []));
    await api.archive('P', 'tiles');
    expect(lastCall().url).toBe('/api/projects/P/archive?q=tiles');
    await api.archive('P');
    expect(lastCall().url).toBe('/api/projects/P/archive');
  });

  it('sends JSON bodies with the routes the server has', async () => {
    fetchMock.mockImplementation(async () => reply(200, {}));
    await api.move('P', 'P-1', 'Done', 2);
    expect(lastCall().url).toBe('/api/projects/P/cards/P-1/move');
    expect(lastCall().init.method).toBe('POST');
    expect(lastCall().headers.get('Content-Type')).toBe('application/json');
    expect(lastCall().body).toEqual({ column: 'Done', index: 2 });

    await api.move('P', 'P-1', 'Done');
    expect(lastCall().body).toEqual({ column: 'Done' });

    await api.patch('P', 'P-1', { title: 'New' }, 'abc');
    expect(lastCall().init.method).toBe('PATCH');
    expect(lastCall().body).toEqual({ title: 'New', if_match: 'abc' });

    await api.patch('P', 'P-1', { body: 'x' });
    expect(lastCall().body).toEqual({ body: 'x' });

    await api.check('P', 'P-1', 3);
    expect(lastCall().url).toBe('/api/projects/P/cards/P-1/check');
    expect(lastCall().body).toEqual({ n: 3 });

    await api.createCard('P', { title: 'T', column: 'Todo' });
    expect(lastCall().url).toBe('/api/projects/P/cards');
    expect(lastCall().body).toEqual({ title: 'T', column: 'Todo' });

    await api.restore('P', 'P-1', 'Todo');
    expect(lastCall().url).toBe('/api/projects/P/cards/P-1/restore');
    expect(lastCall().body).toEqual({ column: 'Todo' });

    await api.archiveDone('P');
    expect(lastCall().url).toBe('/api/projects/P/archive');
    expect(lastCall().init.method).toBe('POST');

    await api.createProject({ name: 'N', path: '/x/y.md', prefix: 'N' });
    expect(lastCall().url).toBe('/api/projects');
    expect(lastCall().body).toEqual({ name: 'N', path: '/x/y.md', prefix: 'N' });

    await api.putSettings({ port: 4000 });
    expect(lastCall().url).toBe('/api/settings');
    expect(lastCall().init.method).toBe('PUT');
  });

  it('sends the settings hash and renames when saving project settings', async () => {
    fetchMock.mockImplementation(async () => reply(200, { board: {}, appearance: {}, readonly: false }));
    const settings = { name: 'P' } as never;
    await api.putProjectSettings('P', {
      settings,
      if_match: 'h1',
      column_renames: { Todo: 'Next' },
      priority_renames: { P0: 'Urgent' },
    });
    expect(lastCall().url).toBe('/api/projects/P/settings');
    expect(lastCall().init.method).toBe('PUT');
    expect(lastCall().body).toEqual({
      settings: { name: 'P' },
      if_match: 'h1',
      column_renames: { Todo: 'Next' },
      priority_renames: { P0: 'Urgent' },
    });
  });

  it('returns undefined for a 204', async () => {
    fetchMock.mockImplementation(async () => reply(204, ''));
    await expect(api.deleteProject('P')).resolves.toBeUndefined();
  });
});

describe('errors', () => {
  it('throws ApiError with code and current on a 409', async () => {
    const current = { id: 'P-1', hash: 'new' };
    fetchMock.mockImplementation(async () => reply(409, { error: 'conflict: changed', code: 'conflict', current }));
    const err = await api.patch('P', 'P-1', { title: 'x' }, 'old').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    const e = err as ApiError;
    expect(e.status).toBe(409);
    expect(e.code).toBe('conflict');
    expect(e.message).toBe('conflict: changed');
    expect(e.current).toEqual(current);
  });

  it('carries the line of a parse error on a 423', async () => {
    fetchMock.mockImplementation(async () => reply(423, { error: 'bad file', code: 'parse', line: 24 }));
    const e = (await api.board('P').catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(423);
    expect(e.code).toBe('parse');
    expect(e.line).toBe(24);
  });

  it('turns a rejected fetch into a network ApiError with status 0', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(0);
    expect(e.code).toBe('network');
  });

  it('survives a body that fails to read', async () => {
    const res = new Response('x', { status: 200 });
    vi.spyOn(res, 'text').mockRejectedValue(new TypeError('terminated'));
    fetchMock.mockResolvedValue(res);
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(0);
    expect(e.code).toBe('network');
  });

  it('turns a non-JSON error response into an ApiError with code error', async () => {
    fetchMock.mockImplementation(async () => reply(502, '<html>Bad Gateway</html>', 'text/html'));
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(502);
    expect(e.code).toBe('error');
    expect(e.message).not.toBe('');
  });

  it('turns an empty error body into an ApiError with code error', async () => {
    fetchMock.mockImplementation(async () => reply(500, ''));
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e.status).toBe(500);
    expect(e.code).toBe('error');
  });

  it('ignores JSON error bodies of the wrong shape', async () => {
    fetchMock.mockImplementation(async () => reply(500, '[1,2]'));
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e.status).toBe(500);
    expect(e.code).toBe('error');
    expect(e.line).toBeUndefined();
    expect(e.current).toBeUndefined();
  });

  it('treats a success response that is not JSON as an error', async () => {
    fetchMock.mockImplementation(async () => reply(200, '<html>login</html>', 'text/html'));
    const e = (await api.projects().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(200);
    expect(e.code).toBe('error');
  });
});
