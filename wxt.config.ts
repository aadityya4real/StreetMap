import { defineConfig } from 'wxt';
import react from '@vitejs/plugin-react';

export default defineConfig({
  srcDir: 'src',
  modules: [],
  vite: () => ({
    plugins: [react()],
  }),
  manifest: {
    name: 'StreetMap',
    description: 'Discover useful local knowledge shared by the community.',
    version: '0.1.0',
    permissions: ['activeTab'],
    icons: {
      16: 'icons/icon-16.png',
      32: 'icons/icon-32.png',
      48: 'icons/icon-48.png',
      128: 'icons/icon-128.png',
    },
    action: {
      default_title: 'Open StreetMap',
    },
  },
});
