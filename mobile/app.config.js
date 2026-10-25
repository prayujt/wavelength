module.exports = ({ config }) => ({
  ...config,
  experiments: {
    ...config.experiments,
    ...(process.env.WAVELENGTH_PWA === '1' ? { baseUrl: '/app' } : {}),
  },
});
