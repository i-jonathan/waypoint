import type {
  BoardResponse,
  BoardView,
  CardPatch,
  CardView,
  GlobalSettings,
  GlobalSettingsChange,
  NewCard,
  NewProject,
  ProjectInfo,
  ProjectSettingsChange,
} from './types';

/**
 * A failed request. `status` is the HTTP status, or 0 when the server could not
 * be reached (code `network`). `code` is the API's error code (`not_found`,
 * `conflict`, `parse`, `invalid`, ...), or `error` when the response was not
 * the API's own JSON, such as one from a proxy.
 */
export class ApiError extends Error {
  status: number;
  code: string;
  /** The line of the board file a `parse` error (423) points at. */
  line?: number;
  /**
   * What a 409 carries to merge against: the card for a card change, the board
   * for a settings change.
   */
  current?: CardView | BoardView;

  constructor(status: number, code: string, message: string, extra: { line?: number; current?: CardView | BoardView } = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    if (extra.line !== undefined) this.line = extra.line;
    if (extra.current !== undefined) this.current = extra.current;
  }
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function failure(status: number, statusText: string, body: unknown): ApiError {
  const fallback = statusText ? `${status} ${statusText}` : `HTTP ${status}`;
  if (!isObject(body)) return new ApiError(status, 'error', fallback);
  const message = typeof body.error === 'string' && body.error !== '' ? body.error : fallback;
  const code = typeof body.code === 'string' && body.code !== '' ? body.code : 'error';
  const extra: { line?: number; current?: CardView | BoardView } = {};
  if (typeof body.line === 'number') extra.line = body.line;
  if (isObject(body.current)) extra.current = body.current as unknown as CardView | BoardView;
  return new ApiError(status, code, message, extra);
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Waypoint': '1' };
  const init: RequestInit = { method, headers };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }

  let res: Response;
  let text: string;
  try {
    res = await fetch(path, init);
    text = await res.text();
  } catch (e) {
    throw new ApiError(0, 'network', e instanceof Error ? e.message : 'network error');
  }

  const parsed = text === '' ? undefined : parseJSON(text);
  if (!res.ok) throw failure(res.status, res.statusText, parsed);
  if (res.status === 204) return undefined as T;
  if (parsed === undefined) throw new ApiError(res.status, 'error', 'the server sent a response that is not JSON');
  return parsed as T;
}

const enc = encodeURIComponent;
const projectPath = (p: string) => `/api/projects/${enc(p)}`;
const cardPath = (p: string, id: string) => `${projectPath(p)}/cards/${enc(id)}`;

function withQuery(path: string, q?: string): string {
  if (!q) return path;
  return `${path}?${new URLSearchParams({ q }).toString()}`;
}

/** One method per API route. */
export const api = {
  projects: () => request<ProjectInfo[]>('GET', '/api/projects'),
  createProject: (project: NewProject) => request<ProjectInfo>('POST', '/api/projects', project),
  deleteProject: (p: string) => request<void>('DELETE', projectPath(p)),

  settings: () => request<GlobalSettings>('GET', '/api/settings'),
  putSettings: (change: GlobalSettingsChange) => request<GlobalSettings>('PUT', '/api/settings', change),

  board: (p: string, q?: string) => request<BoardResponse>('GET', withQuery(`${projectPath(p)}/board`, q)),
  archive: (p: string, q?: string) => request<CardView[]>('GET', withQuery(`${projectPath(p)}/archive`, q)),
  putProjectSettings: (p: string, change: ProjectSettingsChange) =>
    request<BoardResponse>('PUT', `${projectPath(p)}/settings`, change),

  createCard: (p: string, card: NewCard) => request<CardView>('POST', `${projectPath(p)}/cards`, card),
  /** `ifMatch` is the card's `hash`: a stale one is refused with a 409 that carries `current`. */
  patch: (p: string, id: string, patch: CardPatch, ifMatch?: string) =>
    request<CardView>('PATCH', cardPath(p, id), ifMatch === undefined ? patch : { ...patch, if_match: ifMatch }),
  /** Without `index` the card goes to the bottom of the column. */
  move: (p: string, id: string, column: string, index?: number) =>
    request<CardView>('POST', `${cardPath(p, id)}/move`, index === undefined ? { column } : { column, index }),
  check: (p: string, id: string, n: number) => request<CardView>('POST', `${cardPath(p, id)}/check`, { n }),
  restore: (p: string, id: string, column?: string) =>
    request<CardView>('POST', `${cardPath(p, id)}/restore`, column === undefined ? {} : { column }),
  deleteCard: (p: string, id: string) => request<void>('DELETE', cardPath(p, id)),
  archiveDone: (p: string) => request<{ archived: string[] }>('POST', `${projectPath(p)}/archive`),
};
