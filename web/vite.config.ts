import { defineConfig } from 'vite';

export default defineConfig({
  server: {
    port: 3000,
    proxy: {
      '/search': 'http://localhost:8080',
      '/index': 'http://localhost:8080',
      '/upload': 'http://localhost:8080',
      '/files': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    minify: 'esbuild',
    target: 'es2015',
  },
});
