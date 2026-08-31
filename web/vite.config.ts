import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// VITE_API_BASE 在构建时注入，供浏览器访问网关。
export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
  },
  define: {
    __API_BASE__: JSON.stringify(process.env.VITE_API_BASE || 'http://localhost:8080'),
  },
});

