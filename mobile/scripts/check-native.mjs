import { execFileSync } from 'node:child_process';
import assert from 'node:assert/strict';
const config = JSON.parse(
  execFileSync(
    process.execPath,
    ['node_modules/expo/bin/cli', 'config', '--type', 'introspect', '--json'],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
  ),
);
const mods = config._internal.modResults,
  ios = mods.ios.infoPlist,
  manifest = mods.android.manifest.manifest;
const permissions = manifest['uses-permission']
  .filter((p) => p.$['tools:node'] !== 'remove')
  .map((p) => p.$['android:name']);
assert(ios.UIBackgroundModes.includes('audio'), 'iOS background playback is missing');
assert(!('NSMicrophoneUsageDescription' in ios), 'The music player must not request a microphone');
assert(
  !permissions.includes('android.permission.RECORD_AUDIO'),
  'Android microphone permission must be removed',
);
assert(
  permissions.includes('android.permission.FOREGROUND_SERVICE_MEDIA_PLAYBACK'),
  'Android playback service permission is missing',
);
assert(
  manifest.application[0].service.some((s) => s.$['android:name'].includes('AudioControlsService')),
  'Android audio controls service is missing',
);
assert(
  ios.CFBundleURLTypes.some((type) => type.CFBundleURLSchemes.includes('wavelength')),
  'The native sign-in return scheme is missing',
);
console.log(
  'Native configuration: background audio, lock-screen service, callback scheme and microphone exclusion verified.',
);
