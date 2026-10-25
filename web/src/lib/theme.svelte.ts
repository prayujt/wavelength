class Theme {
  dark = $state(false);
  constructor() {
    let preference: string | null = null;
    try {
      preference = localStorage.getItem('wavelength.theme');
    } catch {
      /* Storage can be disabled. */
    }
    this.dark = preference
      ? preference === 'dark'
      : matchMedia('(prefers-color-scheme: dark)').matches;
    document.documentElement.dataset.theme = this.dark ? 'dark' : 'light';
  }
  toggle() {
    this.dark = !this.dark;
    const value = this.dark ? 'dark' : 'light';
    document.documentElement.dataset.theme = value;
    try {
      localStorage.setItem('wavelength.theme', value);
    } catch {
      /* Keep the in-memory preference. */
    }
  }
}
export const theme = new Theme();
