import { afterEach, expect, it, vi } from 'vitest';
import { exclusive } from '../../src/lib/device-store';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('cancels a queued credential lock without running the credential operation', async () => {
  vi.useFakeTimers();
  const operation = vi.fn(async () => 'saved');
  const request = vi.fn((_name: string, options: LockOptions) =>
    new Promise((_resolve, reject) => {
      options.signal?.addEventListener('abort', () => reject(options.signal?.reason), {
        once: true,
      });
    }),
  );
  vi.stubGlobal('navigator', { locks: { request } });
  const result = exclusive('credentials', operation);
  const rejection = expect(result).rejects.toThrow('Another window is connecting. Try again.');
  await vi.advanceTimersByTimeAsync(65000);
  await rejection;
  expect(request.mock.calls[0]?.[0]).toBe('wavelength:credentials');
  expect(request.mock.calls[0]?.[1].signal?.aborted).toBe(true);
  expect(operation).not.toHaveBeenCalled();
});

it('does not time out the credential operation once the lock is acquired', async () => {
  vi.useFakeTimers();
  const request = vi.fn((_name: string, _options: LockOptions, run: () => Promise<string>) => run());
  vi.stubGlobal('navigator', { locks: { request } });
  const result = exclusive('credentials', async () => {
    await new Promise((resolve) => setTimeout(resolve, 70000));
    return 'saved';
  });
  await vi.advanceTimersByTimeAsync(70000);
  await expect(result).resolves.toBe('saved');
  expect(request.mock.calls[0]?.[1].signal?.aborted).toBe(false);
});
