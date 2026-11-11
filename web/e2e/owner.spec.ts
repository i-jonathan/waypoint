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

test('a project can be exported as a file from its settings', async ({ page, wp }) => {
  await wp.cli('add', 'Measure every room', '--body', 'Bring a tape.');
  await page.goto(`${wp.boardURL}/settings`);

  for (const [name, ext, text] of [
    ['Export CSV', '.csv', 'Measure every room'],
    ['Export JSON', '.json', '"title": "Measure every room"'],
    ['Export Markdown', '.md', '### T-1 Measure every room'],
  ]) {
    const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name }).click()]);
    expect(download.suggestedFilename()).toBe(`e2e${ext}`);
    expect(readFileSync((await download.path())!, 'utf8')).toContain(text);
  }
});

test('importing a CSV file checks it, asks before creating a column, and adds the cards', async ({ page, wp }) => {
  await wp.cli('add', 'Already here');
  await page.goto(`${wp.boardURL}/settings`);

  const csv = 'title,column,group\nPaint the hallway,Review,Decor\nBuy brushes,Todo,Decor\n';
  await page.getByLabel('Import file').setInputFiles({ name: 'plan.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) });

  const dialog = page.getByRole('alertdialog', { name: 'Import plan.csv' });
  await expect(dialog.getByText('Adds 2 cards and updates 0 cards.')).toBeVisible();
  await expect(dialog.getByText('Adds columns: Review')).toBeVisible();
  await expect(dialog.getByText('Adds groups: Decor')).toBeVisible();
  // Nothing has happened yet.
  expect(await wp.cli('ls')).not.toContain('Paint the hallway');

  await dialog.getByRole('button', { name: 'Create and import' }).click();
  await expect(page.getByRole('status', { name: 'Import result' })).toContainText('Imported plan.csv: added 2 cards');

  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-2"]')).toContainText('Paint the hallway');
  await expect(page.getByRole('heading', { name: /Review/ })).toBeVisible();
  expect(await wp.cli('ls')).toContain('Buy brushes');
});

test('a bad CSV file is refused with its line and changes nothing', async ({ page, wp }) => {
  await wp.cli('add', 'Already here');
  await page.goto(`${wp.boardURL}/settings`);
  await page.getByLabel('Import file').setInputFiles({ name: 'bad.csv', mimeType: 'text/csv', buffer: Buffer.from('title,priority\nGood,P0\nBad,P9\n') });

  const dialog = page.getByRole('alertdialog', { name: 'Import bad.csv' });
  await expect(dialog.getByRole('alert')).toContainText('line 3');
  await expect(dialog.getByRole('button', { name: 'Import' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(await wp.cli('ls')).not.toContain('Good');
});

test('a Postgres project can be made in the dialog, used, and moved to SQLite and back', async ({ page, wp }) => {
  const pg = process.env.WAYPOINT_TEST_POSTGRES;
  test.skip(!pg, 'WAYPOINT_TEST_POSTGRES is not set');
  const [host, port, user] = (pg ?? '').split(':');
  const database = `waypoint_e2e_${Math.random().toString(16).slice(2, 10)}`;

  await page.goto(wp.boardURL);
  await page.getByRole('button', { name: /Switch project/ }).click();
  await page.getByRole('menuitem', { name: 'New project' }).click();
  const dialog = page.getByRole('dialog', { name: 'New project' });
  await dialog.getByLabel('Name').fill('Server');
  await dialog.getByRole('radio', { name: /Database server/ }).check();
  await expect(dialog).toContainText('WAYPOINT_DB_PASSWORD_SERVER');
  await expect(dialog.getByRole('button', { name: 'Create project' })).toBeDisabled();
  await dialog.getByLabel('Host').fill(host);
  await dialog.getByLabel('Port').fill(port);
  await dialog.getByLabel('User').fill(user);
  await dialog.getByLabel('Database', { exact: true }).fill(database);
  await dialog.getByLabel('Create the database if it does not exist').check();
  await dialog.getByRole('button', { name: 'Test connection' }).click();
  await expect(dialog.getByRole('status')).toContainText('Connected');
  await dialog.getByRole('button', { name: 'Create project' }).click();

  await expect(page).toHaveURL(/\/p\/Server/);
  await page.keyboard.press('n');
  await page.keyboard.type('Lives in Postgres');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();
  expect(await wp.cli('ls', '--project', 'Server')).toContain('Lives in Postgres');
  expect(await wp.cli('project', 'ls')).toContain(`${host}:${port}/${database}`);

  // Out to SQLite, then back into a second, empty database.
  await page.goto(`${wp.url}/p/Server/settings`);
  await page.getByRole('button', { name: 'Convert to SQLite' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Convert' }).click();
  await expect(page.getByRole('status', { name: 'Conversion result' })).toContainText('marked as retired');
  expect(await wp.cli('ls', '--project', 'Server')).toContain('Lives in Postgres');

  await page.getByRole('button', { name: 'Convert to Postgres' }).click();
  const again = page.getByRole('alertdialog', { name: 'Convert this project to Postgres?' });
  await again.getByLabel('Host').fill(host);
  await again.getByLabel('Port').fill(port);
  await again.getByLabel('User').fill(user);
  await again.getByLabel('Database', { exact: true }).fill(`${database}_again`);
  await again.getByLabel('Create the database if it does not exist').check();
  await again.getByRole('button', { name: 'Test connection' }).click();
  await expect(again.getByRole('status')).toContainText('Connected');
  await again.getByRole('button', { name: 'Convert to Postgres' }).click();
  await expect(page.getByRole('status', { name: 'Conversion result' })).toContainText('Converted to Postgres');
  expect(await wp.cli('ls', '--project', 'Server')).toContain('Lives in Postgres');
});
