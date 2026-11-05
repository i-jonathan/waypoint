import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { drag, expect, section, test } from './fixtures';

test('Keep mine saves my notes on top of an external edit', async ({ page, wp }) => {
  await wp.cli('add', 'Shared card', '--body', 'original notes');
  await page.goto(`${wp.boardURL}/card/T-1`);
  await page.getByRole('button', { name: 'Edit' }).click();
  await page.getByRole('textbox', { name: 'Notes in Markdown' }).fill('my notes');

  wp.write(wp.read().replace('### T-1 Shared card', '### T-1 Renamed outside').replace('original notes', 'their notes'));
  await page.getByRole('button', { name: 'Save', exact: true }).click();

  const dialog = page.getByRole('alertdialog', { name: 'This card changed outside Waypoint' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep mine' }).click();
  await expect(dialog).toHaveCount(0);

  await expect.poll(() => wp.read()).toContain('my notes');
  const board = wp.read();
  expect(board).toContain('### T-1 Renamed outside');
  expect(board).not.toContain('their notes');
});

test('an old BOARD.md beside the registered board is left alone', async ({ page, wp }) => {
  const old = join(wp.dir, 'BOARD.md');
  const text = '# Old board\n\n- [ ] something from before Waypoint\n';
  writeFileSync(old, text);

  await wp.cli('add', 'From the CLI');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();
  await page.keyboard.press('n');
  await page.keyboard.type('From the UI');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-card-id="T-2"]')).toBeVisible();

  expect(wp.read()).toContain('From the UI');
  expect(readFileSync(old, 'utf8')).toBe(text);
});

test('a drop on a filtered board leaves the hidden cards in order', async ({ page, wp }) => {
  await wp.cli('add', 'One', '--priority', 'P0');
  await wp.cli('add', 'Two');
  await wp.cli('add', 'Three', '--priority', 'P0');
  await wp.cli('add', 'Four');
  await page.goto(wp.boardURL);
  const first = page.locator('[data-card-id="T-1"]');
  await expect(first).toBeVisible();
  await page.getByRole('textbox', { name: 'Filter cards' }).fill('p0');
  await expect(page.locator('[data-card-id="T-2"]')).toHaveCount(0);

  const box = await first.boundingBox();
  if (!box) throw new Error('no layout');
  await drag(page, page.locator('[data-card-id="T-3"]'), box.x + box.width / 2, box.y + 4);

  const order = () => [...section(wp.read(), 'Todo').matchAll(/^### (T-\d+)/gm)].map((m) => m[1]);
  await expect.poll(order).toEqual(['T-3', 'T-1', 'T-2', 'T-4']);
});

test.describe('reduced motion', () => {
  test.use({ contextOptions: { reducedMotion: 'reduce' } });

  test('a card move runs no animation', async ({ page, wp }) => {
    await wp.cli('add', 'Still');
    await page.goto(wp.boardURL);
    const card = page.locator('[data-card-id="T-1"]');
    await card.focus();
    await page.keyboard.press('Shift+ArrowRight');
    await expect(page.locator('[data-column="In progress"] [data-card-id="T-1"]')).toBeVisible();
    const running = await page.evaluate(() => document.getAnimations().filter((a) => a.playState === 'running').length);
    expect(running).toBe(0);
  });
});

test('a foreign origin is refused and the page sends a CSP', async ({ wp }) => {
  await wp.cli('add', 'Safe');
  const before = wp.read();
  const headers = { 'X-Waypoint': '1', 'Content-Type': 'application/json', Origin: 'http://evil.example' };

  const read = await fetch(`${wp.url}/api/projects/e2e/board`, { headers });
  expect(read.status).toBe(403);
  const write = await fetch(`${wp.url}/api/projects/e2e/cards`, { method: 'POST', headers, body: '{"title":"pwned"}' });
  expect(write.status).toBe(403);
  expect(wp.read()).toBe(before);

  const csp = (await fetch(`${wp.url}/`)).headers.get('content-security-policy');
  expect(csp).toContain("default-src 'self'");
});

test('a hostile card body does not run', async ({ page, wp }) => {
  const body = ['<script>window.pwned = 1</script>', '<img src=x onerror="window.pwned = 2">', '[link](javascript:window.pwned=3)'].join('\n\n');
  await wp.cli('add', 'Hostile', '--body', body);
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto(`${wp.boardURL}/card/T-1`);
  await expect(page.getByRole('region', { name: 'Notes' })).toBeVisible();
  await page.getByRole('region', { name: 'Notes' }).getByRole('link').first().click({ timeout: 1000 }).catch(() => {});

  expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined();
  expect(errors).toEqual([]);
});
