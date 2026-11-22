import { cpSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import { moduleBuild, moduleDev } from '@prusa/connect-ui/build';

// The bundle is written to `dist/`, which release.sh copies into the release
// as `ui/`. So the entry is `ui/index.js` in the archive, the path module.yaml
// names, and the catalogues sit beside it as `ui/locales/`, where the host
// reads them. The manifest is the release's own, at its root.
const LOCALES = fileURLToPath(new URL('./src/locales/', import.meta.url));
const OUT_LOCALES = fileURLToPath(new URL('./dist/locales/', import.meta.url));

const copyLocales = () => ({
  name: 'spool-sync:locales',
  closeBundle: {
    order: 'post' as const,
    handler: () => {
      mkdirSync(OUT_LOCALES, { recursive: true });
      cpSync(LOCALES, OUT_LOCALES, { recursive: true });
    },
  },
});

const bundle = moduleBuild({ entry: 'src/index.ts', outDir: 'dist' });

// `pnpm dev` runs the module's own dev server in front of the core. `pnpm
// build` writes the release bundle.
export default ({ command }: { command: 'build' | 'serve' }) =>
  command === 'serve'
    ? moduleDev()
    : { ...bundle, plugins: [...(bundle.plugins ?? []), copyLocales()] };
