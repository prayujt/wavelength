import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';

execFileSync('buf', ['generate', '../proto', '--template', 'buf.gen.yaml'], {
  stdio: 'inherit',
});
for (const name of [
  'library',
  'ingestion',
  'realtime',
  'sharing',
  'listening',
]) {
  const path = `src/gen/wavelength/v1/${name}_pb.ts`;
  // Keep generated files consistent with the repository's whitespace checks.
  writeFileSync(path, readFileSync(path, 'utf8').trimEnd() + '\n');
}
