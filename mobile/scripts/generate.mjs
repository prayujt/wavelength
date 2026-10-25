import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
execFileSync('buf', ['generate', '../proto', '--template', 'buf.gen.yaml'], { stdio: 'inherit' });
for (const name of ['library', 'ingestion', 'sharing', 'listening', 'realtime']) {
  const path = `src/gen/wavelength/v1/${name}_pb.ts`;
  writeFileSync(path, readFileSync(path, 'utf8').trimEnd() + '\n');
}
