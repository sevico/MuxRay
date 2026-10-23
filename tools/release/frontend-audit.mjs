// Run after the production build from desktop/frontend. No frontend files are written.
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
const output = path.resolve(process.argv[2]);
const { build } = await import(pathToFileURL(path.join(process.cwd(), 'node_modules/vite/dist/node/index.js')));
await build({ configLoader: 'native', cacheDir: path.join(path.dirname(output), '.vite-cache'),
  build: { write: false }, plugins: [{
    name: 'record-production-module-licenses',
    generateBundle(options, bundle) {
      fs.writeFileSync(output, JSON.stringify(Object.values(bundle).filter(v => v.type === 'chunk').map(v => ({
        fileName: v.fileName,
        modules: Object.entries(v.modules).map(([id, m]) => ({ id, renderedLength: m.renderedLength })),
      })), null, 2) + '\n');
    },
  }],
});
