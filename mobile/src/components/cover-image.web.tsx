import React, { memo, useEffect, useRef, useState } from 'react';
import type { CoverSource } from './cover-image';

// Authenticated images need a fetch before <img> can display them. Do that only
// near the viewport, once per source, rather than letting every offscreen cover
// compete with the first audio request. Own and release each temporary URL.
export const CoverImage = memo(function CoverImage({ source }: { source: CoverSource }) {
  const image = useRef<HTMLImageElement>(null);
  const [uri, setUri] = useState<string>(),
    [loaded, setLoaded] = useState(false);
  useEffect(() => {
    const request = new AbortController();
    let objectURL: string | undefined,
      started = false;
    const load = async () => {
      if (started) return;
      started = true;
      if (!source.headers) {
        setUri(source.uri);
        return;
      }
      try {
        const response = await fetch(source.uri, {
          headers: source.headers,
          credentials: 'omit',
          redirect: 'error',
          signal: request.signal,
        });
        if (!response.ok || !response.headers.get('Content-Type')?.startsWith('image/')) return;
        const blob = await response.blob();
        if (request.signal.aborted) return;
        objectURL = URL.createObjectURL(blob);
        setUri(objectURL);
      } catch {
        /* Keep the local cover if offline, revoked, missing or aborted. */
      }
    };
    const observer =
      typeof IntersectionObserver === 'undefined'
        ? undefined
        : new IntersectionObserver(
            (entries) => {
              if (entries.some((entry) => entry.isIntersecting)) {
                observer?.disconnect();
                void load();
              }
            },
            { rootMargin: '160px' },
          );
    if (observer && image.current) observer.observe(image.current);
    else void load();
    return () => {
      observer?.disconnect();
      request.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [source]);
  return (
    <img
      ref={image}
      src={uri}
      alt=""
      decoding="async"
      onLoad={() => setLoaded(true)}
      onError={() => setLoaded(false)}
      style={{
        position: 'absolute',
        inset: 0,
        width: '100%',
        height: '100%',
        objectFit: 'cover',
        opacity: loaded ? 1 : 0,
      }}
    />
  );
});
