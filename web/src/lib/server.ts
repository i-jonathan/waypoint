// Helpers for projects stored on a database server. The password is never
// handled here: the server reads it from its environment, and the UI only tells
// the owner which variable holds it.

export const GLOBAL_PASSWORD_VAR = 'WAYPOINT_DB_PASSWORD';

export const NON_LOCAL_WARNING =
  "This server is not on this computer. Waypoint is designed for one person's data, not for sharing a board between people. Connect only to a server you control.";

/** The variable that holds one project's password: its name in capitals, other characters as _. */
export function passwordVar(project: string): string {
  let out = `${GLOBAL_PASSWORD_VAR}_`;
  for (const ch of project.toUpperCase()) out += /[A-Z0-9]/.test(ch) ? ch : '_';
  return out;
}

/** Whether a host is this computer: localhost, 127.0.0.0/8, ::1 or a Unix socket path. */
export function isLocalHost(host: string): boolean {
  const h = host.trim().replace(/^\[|\]$/g, '');
  if (h.startsWith('/')) return true;
  if (h.toLowerCase() === 'localhost') return true;
  if (h === '::1') return true;
  const m = /^127\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(h);
  return m !== null && m.slice(1).every((n) => Number(n) <= 255);
}

export const DEFAULT_PORTS = { postgres: 5432, mysql: 3306 } as const;
export type ServerEngine = keyof typeof DEFAULT_PORTS;

/** The database servers a project can live on, as the owner sees them. */
export const SERVER_ENGINES: readonly { value: ServerEngine; label: string }[] = [
  { value: 'postgres', label: 'Postgres' },
  { value: 'mysql', label: 'MySQL' },
];

export const engineLabel = (engine: string): string => SERVER_ENGINES.find((e) => e.value === engine)?.label ?? engine;
export const isServerEngine = (storage: string): storage is ServerEngine => SERVER_ENGINES.some((e) => e.value === storage);

export const SSL_CHOICES = [
  { value: '', label: 'Default (encrypted unless the server is on this computer)' },
  { value: 'disable', label: 'Off' },
  { value: 'prefer', label: 'Use it if the server offers it' },
  { value: 'require', label: 'Required' },
  { value: 'verify-full', label: 'Required, and check the certificate' },
] as const;
