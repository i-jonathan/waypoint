// Paints the last look before first paint, so a light or accent-coloured
// board does not flash dark. src/lib/theme.ts (applyLook) does the same
// work once the app has loaded; tests/theme.test.ts keeps the two equal.
// A file rather than an inline script, which the Content-Security-Policy refuses.
(function () {
  var r = document.documentElement,
    s = null;
  try {
    s = JSON.parse(localStorage.getItem('waypoint.look'));
  } catch (e) {}
  var t = s && s.theme;
  t = t === 'light' || t === 'dark' ? t : matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  r.dataset.theme = t;
  if (s && s[t] && s.density && s.textures) {
    r.dataset.density = s.density;
    r.dataset.textures = s.textures;
    r.style.setProperty('--acc', s[t].acc);
    r.style.setProperty('--acc-ink', s[t].ink);
  }
})();
