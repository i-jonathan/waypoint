import { afterEach, describe, expect, it } from 'vitest';
import { displayDate } from '../src/lib/dates';

const originalTZ = process.env.TZ;

function inZone(tz: string, fn: () => void) {
  process.env.TZ = tz;
  fn();
}

afterEach(() => {
  if (originalTZ === undefined) delete process.env.TZ;
  else process.env.TZ = originalTZ;
});

describe('displayDate', () => {
  it('shows null as an empty string', () => {
    expect(displayDate(null)).toBe('');
  });

  it('shows a date-only value verbatim in any zone', () => {
    for (const tz of ['Pacific/Auckland', 'America/Los_Angeles', 'UTC']) {
      inZone(tz, () => expect(displayDate('2026-10-26')).toBe('2026-10-26'));
    }
  });

  it('shows a timestamp at exactly 00:00:00Z verbatim in any zone', () => {
    for (const tz of ['Pacific/Auckland', 'America/Los_Angeles', 'UTC']) {
      inZone(tz, () => expect(displayDate('2026-11-04T00:00:00Z')).toBe('2026-11-04'));
    }
  });

  it('shows any other timestamp as the local date, ahead of UTC', () => {
    // 23:52 UTC on the 4th is already the 5th in Auckland (UTC+13 in November).
    inZone('Pacific/Auckland', () => expect(displayDate('2026-11-04T23:52:10Z')).toBe('2026-11-05'));
  });

  it('shows any other timestamp as the local date, behind UTC', () => {
    // One second after midnight UTC is still the 3rd in Los Angeles.
    inZone('America/Los_Angeles', () => expect(displayDate('2026-11-04T00:00:01Z')).toBe('2026-11-03'));
    // 23:59:59 UTC on the 4th is the 4th in Los Angeles.
    inZone('America/Los_Angeles', () => expect(displayDate('2026-11-04T23:59:59Z')).toBe('2026-11-04'));
  });

  it('leaves a value it cannot read as it is', () => {
    expect(displayDate('someday')).toBe('someday');
  });
});
