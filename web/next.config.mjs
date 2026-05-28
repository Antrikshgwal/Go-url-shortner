const BACKEND = process.env.BACKEND_ORIGIN || "https://snip-seo2.onrender.com";

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  async rewrites() {
    // Proxy browser API calls through the Next server to dodge CORS,
    // since the Go backend sends no Access-Control-Allow-Origin header.
    return [{ source: "/api/:path*", destination: `${BACKEND}/:path*` }];
  },
};

export default nextConfig;
