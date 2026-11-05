import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

export const repoRoot = resolve(import.meta.dirname, '../..');
/** Everything a test writes lives here, including the binary. */
export const scratchRoot = join(tmpdir(), 'waypoint-e2e');
export const binaryPath = join(scratchRoot, 'waypoint');
