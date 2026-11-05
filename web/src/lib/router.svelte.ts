export type RouteName = 'home' | 'board' | 'next' | 'blocked' | 'archive' | 'card' | 'settings' | 'projectSettings';

export interface Route {
  name: RouteName;
  project?: string;
  id?: string;
  /** The query string as written, with its leading '?', or ''. Kept encoded. */
  search: string;
}

const PROJECT_VIEWS: Record<string, RouteName> = {
  next: 'next',
  blocked: 'blocked',
  archive: 'archive',
  settings: 'projectSettings',
};

/** Reads a path such as `/p/Home%20reno/card/H-4?q=x`. An unknown path is home. */
export function parsePath(path: string): Route {
  const q = path.indexOf('?');
  const hash = path.indexOf('#');
  const end = hash === -1 ? path.length : hash;
  const pathname = q === -1 || q > end ? path.slice(0, end) : path.slice(0, q);
  const search = q === -1 || q > end ? '' : path.slice(q, end);
  const home: Route = { name: 'home', search };

  let parts: string[];
  try {
    parts = pathname
      .split('/')
      .filter((s) => s !== '')
      .map(decodeURIComponent);
  } catch {
    return home; // a malformed % escape
  }

  if (parts.length === 0) return home;
  if (parts.length === 1 && parts[0] === 'settings') return { name: 'settings', search };
  if (parts[0] !== 'p' || parts[1] === undefined) return home;

  const project = parts[1];
  if (parts.length === 2) return { name: 'board', project, search };
  const view = parts[2]!;
  if (parts.length === 3 && Object.hasOwn(PROJECT_VIEWS, view)) return { name: PROJECT_VIEWS[view]!, project, search };
  if (parts.length === 4 && view === 'card') return { name: 'card', project, id: parts[3]!, search };
  return home;
}

/** The path for a route, with every name encoded. The inverse of parsePath. */
export function href(r: Route): string {
  const base = r.project === undefined ? '' : `/p/${encodeURIComponent(r.project)}`;
  let path: string;
  switch (r.name) {
    case 'home':
      path = '/';
      break;
    case 'settings':
      path = '/settings';
      break;
    case 'board':
      path = base;
      break;
    case 'projectSettings':
      path = `${base}/settings`;
      break;
    case 'card':
      path = `${base}/card/${encodeURIComponent(r.id ?? '')}`;
      break;
    default:
      path = `${base}/${r.name}`;
  }
  return path + r.search;
}

/** The current route. The card route renders the board with the panel open. */
export const route: Route = $state({ name: 'home', search: '' });

function apply(next: Route) {
  route.name = next.name;
  route.project = next.project;
  route.id = next.id;
  route.search = next.search;
}

function current(): string {
  return window.location.pathname + window.location.search + window.location.hash;
}

/**
 * Asks before the route changes. A guard returns true to let the move happen, or
 * false to stop it; it may then offer the move again itself once the owner agrees.
 */
export type NavigationGuard = (to: string) => boolean;

let guard: NavigationGuard | null = null;

/** Installs a guard, and returns the function that removes it. One guard at a time: the newest wins. */
export function guardNavigation(fn: NavigationGuard): () => void {
  guard = fn;
  return () => {
    if (guard === fn) guard = null;
  };
}

/** Goes to a path, which may carry a query string. A guard may refuse. */
export function navigate(path: string) {
  if (path === current()) {
    apply(parsePath(path));
    return;
  }
  if (guard && !guard(path)) return;
  window.history.pushState(null, '', path);
  shown = current();
  apply(parsePath(shown));
}

/** The path on screen, which a refused Back or Forward puts back. */
let shown = typeof window === 'undefined' ? '/' : current();

if (typeof window !== 'undefined') {
  apply(parsePath(current()));
  window.addEventListener('popstate', () => {
    const to = current();
    if (guard && !guard(to)) {
      // The browser has already moved; push the page it left back on top, so the form stays where it was.
      window.history.pushState(null, '', shown);
      return;
    }
    shown = to;
    apply(parsePath(to));
  });
}
