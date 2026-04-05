import { defineConfig } from "vite";
import fs from "fs";
import path from "path";

export default defineConfig({
  server: {
    port: 3000,
    proxy: {
      "/search": "http://localhost:8080",
      "/index": "http://localhost:8080",
      "/upload": "http://localhost:8080",
      "/files": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    minify: "esbuild",
    target: "es2015",
  },
  plugins: [
    {
      name: "custom-404",
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          const url = req.url?.split("?")[0] || "";

          if (
            url === "/" ||
            url === "" ||
            url.startsWith("/search") ||
            url.startsWith("/upload") ||
            url.startsWith("/files") ||
            url.startsWith("/stats") ||
            url.startsWith("/crawl")
          ) {
            return next();
          }

          if (path.extname(url)) {
            return next();
          }

          res.statusCode = 404;
          res.setHeader("Content-Type", "text/html");
          res.end(fs.readFileSync(path.join(process.cwd(), "404.html")));
        });
      },
    },
  ],
});
