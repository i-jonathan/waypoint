<script lang="ts">
  import { accentInk, resolveAccent } from './lib/accent';

  // Built-in appearance defaults. Settings will replace these.
  const accent = 'orange';
  const density = 'comfortable';
  const textures = 'on';

  const scheme = window.matchMedia('(prefers-color-scheme: light)');

  function apply() {
    const theme = scheme.matches ? 'light' : 'dark';
    const hex = resolveAccent(accent, theme);
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.dataset.density = density;
    root.dataset.textures = textures;
    root.style.setProperty('--acc', hex);
    root.style.setProperty('--acc-ink', accentInk(hex));
  }

  $effect(() => {
    apply();
    scheme.addEventListener('change', apply);
    return () => scheme.removeEventListener('change', apply);
  });
</script>

<main>
  <h1 class="sign">Waypoint</h1>
</main>

<style>
  main {
    padding: 28px;
  }

  h1 {
    margin: 0;
    font-size: 48px;
    color: var(--acc);
  }
</style>
