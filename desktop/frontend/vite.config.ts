import { defineConfig } from 'vite';

export default defineConfig({
  base: './',
  build: { target: 'es2022' },
  // The TypeScript JSX transform is sufficient; no Babel/HMR plugin is needed
  // for this small shell. A production build never starts a dev server.
  esbuild: { jsx: 'automatic' },
});
