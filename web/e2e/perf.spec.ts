import { expect, test, type Waypoint } from './fixtures';

const median = (xs: number[]) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)];

/** 100 cards in each of the three default columns. */
function fill(wp: Waypoint): void {
  let id = 2; // T-1 is the seed card.
  const cards = () => Array.from({ length: 100 }, () => `### T-${id} Generated card ${id++}\n`).join('\n');
  wp.write(wp.read().replace('## Todo\n', `## Todo\n\n${cards()}`).replace('## In progress\n', `## In progress\n\n${cards()}`).replace('## Done\n', `## Done\n\n${cards()}`));
}

test('300 cards render within 300ms of the response (median of 3)', async ({ page, wp }) => {
  await wp.cli('add', 'Seed');
  fill(wp);
  // Everything is timed inside the page from before the app starts: the first board response,
  // then the first frame painted with all 300 cards in the DOM.
  await page.addInitScript(() => {
    const w = window as unknown as { boardAt?: number; paintedAt?: number };
    const real = window.fetch;
    window.fetch = async (...args) => {
      const res = await real(...args);
      if (w.boardAt === undefined && String(args[0]).endsWith('/board')) {
        await res.clone().text();
        w.boardAt = performance.now();
      }
      return res;
    };
    new MutationObserver((_, obs) => {
      if (document.querySelectorAll('[data-card-id]').length < 300) return;
      obs.disconnect();
      requestAnimationFrame(() => requestAnimationFrame(() => (w.paintedAt = performance.now())));
    }).observe(document, { childList: true, subtree: true });
  });

  const runs: number[] = [];
  for (let i = 0; i < 3; i++) {
    await page.goto(wp.boardURL);
    await page.waitForFunction(() => (window as unknown as { paintedAt?: number }).paintedAt !== undefined);
    runs.push(await page.evaluate(() => {
      const w = window as unknown as { boardAt: number; paintedAt: number };
      return w.paintedAt - w.boardAt;
    }));
  }
  console.log(`render after response, ms: ${runs.map(Math.round).join(', ')}`);
  expect(median(runs)).toBeLessThanOrEqual(300);
});

test('a drag across three columns stays smooth with 300 cards', async ({ page, wp }) => {
  await wp.cli('add', 'Seed');
  fill(wp);
  await page.goto(wp.boardURL);
  const card = page.locator('[data-card-id="T-2"]');
  await expect(card).toBeVisible();
  await expect.poll(() => page.locator('[data-card-id]').count()).toBeGreaterThanOrEqual(300);

  await page.evaluate(() => {
    const w = window as unknown as { longTasks: number[]; frames: number[]; dragDone: boolean };
    w.longTasks = [];
    w.frames = [];
    new PerformanceObserver((list) => list.getEntries().forEach((e) => w.longTasks.push(e.duration))).observe({ type: 'longtask' });
    let last: number | undefined;
    const frame = (now: number) => {
      if (last !== undefined) w.frames.push(now - last);
      last = now;
      if (!w.dragDone) requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  });

  const box = await card.boundingBox();
  const end = await page.locator('[data-column="Done"]').boundingBox();
  if (!box || !end) throw new Error('no layout');
  const x0 = box.x + box.width / 2;
  const y0 = box.y + box.height / 2;
  await page.mouse.move(x0, y0);
  await page.mouse.down();
  await page.mouse.move(x0 + 12, y0 + 12, { steps: 3 });
  await expect(page.locator('.drag-clone')).toBeVisible();

  // One pointer move per frame, as a real drag gives, or the moves all land before a frame runs.
  const steps = 90;
  const x1 = end.x + end.width / 2;
  for (let i = 1; i <= steps; i++) {
    await page.mouse.move(x0 + ((x1 - x0) * i) / steps, y0 + 120);
    await page.evaluate(() => new Promise(requestAnimationFrame));
  }
  const m = await page.evaluate(() => {
    const w = window as unknown as { longTasks: number[]; frames: number[]; dragDone: boolean };
    w.dragDone = true;
    return { longTasks: w.longTasks, frames: w.frames };
  });
  await page.mouse.up();

  console.log(`drag: median frame ${median(m.frames).toFixed(1)}ms, worst ${Math.max(...m.frames).toFixed(1)}ms, over ${m.frames.length} frames, long tasks ${JSON.stringify(m.longTasks)}`);
  expect(m.frames.length).toBeGreaterThanOrEqual(steps);
  expect(m.longTasks.filter((d) => d > 50)).toEqual([]);
  // A frame interval can't go below the 16.7ms display tick, so 17ms here means the 16ms budget held.
  expect(median(m.frames)).toBeLessThanOrEqual(17);
});
