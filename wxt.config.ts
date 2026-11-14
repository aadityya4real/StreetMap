import { defineConfig } from 'wxt';
import react from '@vitejs/plugin-react';
import { loadEnv, type Plugin } from 'vite';
import { isSupabaseProjectUrl, validateSupabaseBuildConfig } from './src/popup/auth/config';

const validateSupabaseEnvironmentBeforeBundle: Plugin = {
  name: 'streetmap-validate-supabase-public-config',
  enforce: 'pre',
  configResolved(config) {
    const env = loadEnv(config.mode, config.envDir, 'VITE_');
    validateSupabaseBuildConfig({
      VITE_SUPABASE_URL: env.VITE_SUPABASE_URL,
      VITE_SUPABASE_ANON_KEY: env.VITE_SUPABASE_ANON_KEY,
    });
  },
};

export default defineConfig({
  srcDir: 'src',
  modules: [],
  dev: {
    server: {
      host: '127.0.0.1',
      port: 3002,
      origin: 'http://127.0.0.1:3002',
      strictPort: true,
    },
  },
  vite: () => ({
    plugins: [validateSupabaseEnvironmentBeforeBundle, react()],
  }),
  manifest: ({ mode }) => {
    const supabaseUrl = import.meta.env.VITE_SUPABASE_URL?.trim() ?? '';
    const supabaseHost = isSupabaseProjectUrl(supabaseUrl) ? `${new URL(supabaseUrl).origin}/*` : null;
    return {
      name: 'StreetMap',
      description: 'Discover useful local knowledge shared by the community.',
      version: '0.1.0',
      permissions: ['activeTab', 'scripting', 'identity', 'storage'],
      host_permissions: [
        ...(mode === 'development' ? ['http://127.0.0.1:8080/*'] : []),
        ...(supabaseHost ? [supabaseHost] : []),
      ],
      icons: {
        16: 'icons/icon-16.png',
        32: 'icons/icon-32.png',
        48: 'icons/icon-48.png',
        128: 'icons/icon-128.png',
      },
      action: {
        default_title: 'Open StreetMap',
      },
    };
  },
});
