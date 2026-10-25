// The Node tests exercise the same transport with a controllable standards-based fetch.
export const fetch: typeof globalThis.fetch = (input, init) => globalThis.fetch(input, init);
