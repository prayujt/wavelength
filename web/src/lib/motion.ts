// A background update only draws attention to entries already in view.
export function reveal(node: HTMLElement, enabled: boolean) {
  if (
    !enabled ||
    document.hidden ||
    matchMedia('(prefers-reduced-motion: reduce)').matches
  )
    return;
  let animation: Animation | undefined;
  const observer = new IntersectionObserver(([entry]) => {
    observer.disconnect();
    if (entry.isIntersecting)
      animation = node.animate(
        [
          { opacity: 0, transform: 'translateY(4px)' },
          { opacity: 1, transform: 'translateY(0)' },
        ],
        { duration: 200, easing: 'ease-out' },
      );
  });
  observer.observe(node);
  return {
    destroy() {
      observer.disconnect();
      animation?.cancel();
    },
  };
}
