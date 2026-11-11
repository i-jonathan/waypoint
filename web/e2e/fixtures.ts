import { execFile, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { test as base, expect, type Locator, type Page } from '@playwright/test';
import { binaryPath, scratchRoot } from './paths';

const run = promisify(execFile);

export interface Waypoint {
  /** Root URL of this test's server. */
  url: string;
  /** Board page for the test's project. */
  boardURL: string;
  /** The board file on disk. */
  file: string;
  dir: string;
  read(): string;
  write(text: string): void;
  /** Runs the real CLI against this test's config, in the project folder. */
  cli(...args: string[]): Promise<string>;
  /** Stops the server and waits for it to exit. */
  stop(): Promise<void>;
  /** Starts the server again on the same port. */
  start(): Promise<void>;
}

function freePort(): Promise<number> {
  return new Promise((done, fail) => {
    const s = createServer();
    s.once('error', fail);
    s.listen(0, '127.0.0.1', () => {
      const addr = s.address();
      s.close(() => done(typeof addr === 'object' && addr ? addr.port : 0));
    });
  });
}

async function waitUp(url: string): Promise<void> {
  for (let i = 0; i < 200; i++) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // Not listening yet.
    }
    await new Promise((r) => setTimeout(r, 25));
  }
  throw new Error(`server at ${url} never came up`);
}

export const test = base.extend<{ wp: Waypoint }>({
  wp: async ({}, use) => {
    const dir = mkdtempSync(join(scratchRoot, 't-'));
    const home = join(dir, 'home');
    const config = join(dir, 'config.yaml');
    // A test must never reach the real home directory, so stop here if the paths say otherwise.
    if (dir.startsWith(homedir()) || home === homedir()) throw new Error(`unsafe test dir ${dir}`);

    const env = { ...process.env, HOME: home, WAYPOINT_CONFIG: config, XDG_CONFIG_HOME: join(home, '.config'), XDG_DATA_HOME: join(home, '.data') };
    delete env.WAYPOINT_PROJECT;
    // A Postgres test server's password reaches the server and the CLI the way a real one would: through the environment.
    const dbServer = process.env.WAYPOINT_TEST_POSTGRES;
    if (dbServer) env.WAYPOINT_DB_PASSWORD = dbServer.split(':').slice(3).join(':');
    writeFileSync(config, 'tls: off\n');

    const file = join(dir, 'waypoint.md');
    const cli = async (...args: string[]) => (await run(binaryPath, args, { cwd: dir, env })).stdout;
    await cli('project', 'add', 'e2e', '--path', file, '--prefix', 'T');

    const port = await freePort();
    const url = `http://127.0.0.1:${port}`;
    let server: ChildProcess | undefined;

    const start = async () => {
      server = spawn(binaryPath, ['serve', '--no-open', '--port', String(port)], { cwd: dir, env, stdio: 'ignore' });
      await waitUp(`${url}/`);
    };
    const stop = async () => {
      const s = server;
      server = undefined;
      if (!s || s.exitCode !== null) return;
      const gone = new Promise((r) => s.once('exit', r));
      s.kill('SIGKILL');
      await gone;
    };

    await start();
    try {
      await use({
        url,
        boardURL: `${url}/p/e2e`,
        file,
        dir,
        read: () => readFileSync(file, 'utf8'),
        write: (text) => writeFileSync(file, text),
        cli,
        stop,
        start,
      });
    } finally {
      await stop();
      rmSync(dir, { recursive: true, force: true });
    }
  },
});

export { expect };

/** The text of one `## Column` section of a board file. */
export function section(board: string, column: string): string {
  const start = board.indexOf(`\n## ${column}\n`);
  if (start === -1) return '';
  const next = board.indexOf('\n## ', start + 1);
  return board.slice(start, next === -1 ? undefined : next);
}

/** Drags with a real pointer: past the threshold first, then to the point, in small steps. */
export async function drag(page: Page, card: Locator, x: number, y: number, steps = 12): Promise<void> {
  const box = await card.boundingBox();
  if (!box) throw new Error('card has no layout');
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;
  await page.mouse.move(cx, cy);
  await page.mouse.down();
  await page.mouse.move(cx + 12, cy + 12, { steps: 3 });
  await page.mouse.move(x, y, { steps });
  await page.mouse.up();
}
