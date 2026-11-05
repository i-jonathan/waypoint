import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { binaryPath, repoRoot, scratchRoot } from './paths';

// Builds the UI and the binary once. `make build` isn't used here: it runs `npm ci`, which would
// delete node_modules under the running test runner.
export default function globalSetup(): void {
  mkdirSync(scratchRoot, { recursive: true });
  execFileSync('npm', ['run', 'build'], { cwd: `${repoRoot}/web`, stdio: 'inherit' });
  execFileSync('go', ['build', '-o', binaryPath, './cmd/waypoint'], { cwd: repoRoot, stdio: 'inherit' });
}
