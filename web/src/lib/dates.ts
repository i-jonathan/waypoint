const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;
const MIDNIGHT_UTC = /^(\d{4}-\d{2}-\d{2})T00:00:00Z$/;

/**
 * The calendar date to show for a card's `created` or `done` value, which the
 * API sends as a UTC timestamp (2026-11-04T23:52:10Z), a date-only value
 * (2026-10-26) or null.
 *
 * A date-only value, or a timestamp at exactly 00:00:00Z (which is how a bare
 * date is stored), is shown as written so it never shifts a day. Any other
 * timestamp shows the browser's local date as YYYY-MM-DD. Null shows nothing,
 * and a value that is not a date is returned as it is.
 */
export function displayDate(value: string | null): string {
  if (value === null) return '';
  if (DATE_ONLY.test(value)) return value;
  const midnight = MIDNIGHT_UTC.exec(value);
  if (midnight) return midnight[1]!;
  const t = new Date(value);
  if (Number.isNaN(t.getTime())) return value;
  const y = String(t.getFullYear()).padStart(4, '0');
  const m = String(t.getMonth() + 1).padStart(2, '0');
  const d = String(t.getDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}
