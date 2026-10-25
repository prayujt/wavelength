export function link(page: string, parameters: Record<string, string> = {}) {
  const query = new URLSearchParams(parameters).toString();
  return `#${page}${query ? `?${query}` : ''}`;
}

export function readRoute(hash: string) {
  const [path, query] = hash.replace(/^#\/?/, '').split('?');
  const [page = 'library', id = ''] = (path || 'library').split('/');
  let decoded = '';
  try {
    decoded = decodeURIComponent(id);
  } catch {
    decoded = '';
  }
  return { page, id: decoded, query: new URLSearchParams(query) };
}

export function go(page: string, parameters: Record<string, string> = {}) {
  window.location.hash = link(page, parameters);
}
