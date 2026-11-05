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
  const body = ['Plain text survives', '<script>window.pwned = 1</script>', '<img src=x onerror="window.pwned = 2">', '[link](javascript:window.pwned=3)'].join('\n\n');
  await wp.cli('add', 'Hostile', '--body', body);
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto(`${wp.boardURL}/card/T-1`);
  const notes = page.getByRole('region', { name: 'Notes' });
  await expect(notes).toContainText('Plain text survives');

  // The sanitiser did this, not just the CSP behind it.
  await expect(notes.locator('script')).toHaveCount(0);
  await expect(notes.locator('img[onerror], img[src="x"]')).toHaveCount(0);
  await expect(notes.locator('a[href^="javascript:"]')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined();
  expect(errors).toEqual([]);
});

test('the panel of an archived card follows a hand edit', async ({ page, wp }) => {
  await wp.cli('add', 'Old card', '--body', 'old');
  await wp.cli('move', 'T-1', 'Done');
  await wp.cli('archive');
  const archive = wp.file.replace(/\.md$/, '.archive.md');
  expect(readFileSync(archive, 'utf8')).toContain('old');

  await page.goto(`${wp.boardURL}/card/T-1`);
  await expect(page.getByRole('region', { name: 'Notes' })).toContainText('old');
  writeFileSync(archive, readFileSync(archive, 'utf8').replace('old\n', 'IMPORTANT HAND EDIT\n'));

  await expect(page.getByRole('region', { name: 'Notes' })).toContainText('IMPORTANT HAND EDIT', { timeout: 2000 });
});

test('five hidden tabs leave a connection free for writes', async ({ context, page, wp }) => {
  await wp.cli('add', 'Many tabs');
  const hidden = await Promise.all(Array.from({ length: 5 }, () => context.newPage()));
  for (const tab of hidden) {
    await tab.addInitScript(() => Object.defineProperty(document, 'hidden', { get: () => true }));
    await tab.goto(wp.boardURL);
    await expect(tab.locator('[data-card-id="T-1"]')).toBeVisible();
  }

  await page.goto(wp.boardURL);
  await page.locator('[data-card-id="T-1"]').focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect.poll(() => section(wp.read(), 'In progress'), { timeout: 3000 }).toContain('Many tabs');
});

test('a project made from the dialog with the defaults is a working SQLite board', async ({ page, wp }) => {
  await page.goto(wp.boardURL);
  await page.getByRole('button', { name: /Switch project/ }).click();
  await page.getByRole('menuitem', { name: 'New project' }).click();

  const dialog = page.getByRole('dialog', { name: 'New project' });
  await expect(dialog.getByRole('radio', { name: /SQLite database/ })).toBeChecked();
  await dialog.getByLabel('Name').fill('Lite');
  await dialog.getByRole('button', { name: 'Create project' }).click();

  await expect(page).toHaveURL(/\/p\/Lite/);
  await page.keyboard.press('n');
  await page.keyboard.type('Made in a database');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();

  expect(await wp.cli('project', 'ls')).toMatch(/Lite\s+sqlite\s+\S+lite\.db/);
  expect(await wp.cli('ls', '--project', 'Lite')).toContain('Made in a database');
});

test('converting a project to SQLite from its settings keeps the board working', async ({ page, wp }) => {
  await wp.cli('add', 'Survives the move', '--body', 'with its notes');
  await wp.cli('add', 'Second card', '--depends', 'T-1');
  await page.goto(`${wp.boardURL}/settings`);

  await page.getByRole('button', { name: 'Convert to SQLite' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Convert this project to SQLite?' });
  await expect(dialog).toContainText('waypoint.db');
  await dialog.getByRole('button', { name: 'Convert' }).click();

  const result = page.getByRole('status', { name: 'Conversion result' });
  await expect(result).toContainText('waypoint.md.bak-');
  // The page follows the registry: the type now reads sqlite, and the button offers the way back.
  await expect(page.getByRole('button', { name: 'Convert to Markdown' })).toBeVisible();

  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();
  await expect(page.locator('[data-card-id="T-2"]')).toContainText('Blocked by T-1');
  await page.keyboard.press('n');
  await page.keyboard.type('Added after the move');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-card-id="T-3"]')).toBeVisible();

  expect(await wp.cli('ls')).toContain('Added after the move');
  expect(await wp.cli('project', 'ls')).toMatch(/sqlite\s+\S+waypoint\.db/);
});
