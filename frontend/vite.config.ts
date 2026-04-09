import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig(({ command }) => {
  const backendTarget = process.env.VITE_BACKEND_URL ?? 'http://127.0.0.1:9080';
  const appBase = command === 'build' ? '/app/' : '/';
  const outDir = process.env.AUTO_CODE_FRONTEND_OUT_DIR?.trim() || 'dist';

  return {
    base: appBase,
    plugins: [react()],
    build: {
      outDir,
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      port: 5173,
      host: true,
      proxy: {
        '/api': {
          target: backendTarget,
          changeOrigin: true,
        },
        '/cli': {
          target: backendTarget,
          changeOrigin: true,
        },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      css: false,
      exclude: ['e2e/**/*', 'node_modules/**/*'],
    },
  };
});
