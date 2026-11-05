import { drag, expect, section, test } from './fixtures';

test('create a card, drag it, finish it by keyboard, and filter', async ({ page, wp }) => {
  await wp.cli('add', 'Urgent thing', '--priority', 'P0');
  await page.goto(wp.boardURL);
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();

  await page.keyboard.press('n');
  await page.keyboard.type('Water the plants');
  await page.keyboard.press('Enter');
  const card = page.locator('[data-card-id="T-2"]');
  await expect(card).toBeVisible();
  await page.getByRole('button', { name: 'Close quick add' }).click();

  const target = await page.locator('[data-column="In progress"]').boundingBox();
  if (!target) throw new Error('no layout');
  await drag(page, card, target.x + target.width / 2, target.y + 80);
  await expect.poll(() => section(wp.read(), 'In progress')).toContain('Water the plants');

  await card.focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect.poll(() => section(wp.read(), 'Done')).toContain('Water the plants');
  await expect(card.locator('.t')).toHaveClass(/struck/);

  await page.getByRole('textbox', { name: 'Filter cards' }).fill('p0');
  await expect(page.locator('[data-card-id="T-1"]')).toBeVisible();
  await expect(card).toHaveCount(0);
});
