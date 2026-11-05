import { appendFileSync } from 'node:fs';
import { expect, section, test } from './fixtures';

const LIVE = { timeout: 1500 };

test('a card appended to the file appears with no reload', async ({ page, wp }) => {
  await wp.cli('add', 'First');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();

  const board = wp.read();
  const at = board.indexOf('\n## In progress');
  wp.write(`${board.slice(0, at)}\n### T-7 Added by hand\n${board.slice(at)}`);
  await expect(page.locator('[data-card-id="T-7"]')).toBeVisible(LIVE);
});

test('an unreadable file shows the banner and blocks new cards until it is fixed', async ({ page, wp }) => {
  await wp.cli('add', 'First');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();
  const good = wp.read();

  appendFileSync(wp.file, '\nstray text outside a card\n');
  const banner = page.getByRole('alert').filter({ hasText: "can't be read" });
  await expect(banner).toContainText('line', LIVE);
  await expect(page.getByRole('button', { name: 'New card' })).toBeDisabled();

  wp.write(good);
  await expect(banner).toHaveCount(0, LIVE);
  await expect(page.getByRole('button', { name: 'New card' })).toBeEnabled();
});

test('a server restart shows Reconnecting, then the board catches up', async ({ page, wp }) => {
  await wp.cli('add', 'First');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();

  await wp.stop();
  await expect(page.getByText('Reconnecting')).toBeVisible({ timeout: 10_000 });
  await wp.cli('add', 'While it was down');
  await wp.start();
  await expect(page.getByText('Reconnecting')).toHaveCount(0, { timeout: 15_000 });
  await expect(page.locator('[data-card-id="T-2"]')).toBeVisible();
});

test('cards an agent adds and moves through the CLI appear with no reload', async ({ page, wp }) => {
  await wp.cli('add', 'First');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();

  await wp.cli('add', 'From an agent');
  const card = page.locator('[data-card-id="T-2"]');
  await expect(card).toBeVisible(LIVE);

  await wp.cli('move', 'T-2', 'In progress');
  await expect(page.locator('[data-column="In progress"] [data-card-id="T-2"]')).toBeVisible(LIVE);
  expect(section(wp.read(), 'In progress')).toContain('From an agent');
});
