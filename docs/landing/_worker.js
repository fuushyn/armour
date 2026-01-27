// Cloudflare Pages Function to proxy install script and patterns
const GITHUB_RAW = 'https://raw.githubusercontent.com/fuushyn/armour/main/safehooks';

const PROXY_PATHS = {
  '/install.sh': '/install.sh',
  '/install': '/install.sh',
  '/block-patterns.json': '/block-patterns.json',
  '/allow-patterns.json': '/allow-patterns.json',
  '/tool-rules.json': '/tool-rules.json',
};

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname;

    // Check if this is a path we need to proxy
    const proxyFile = PROXY_PATHS[path];
    if (proxyFile) {
      const response = await fetch(`${GITHUB_RAW}${proxyFile}`);
      const content = await response.text();

      const contentType = path.endsWith('.json')
        ? 'application/json'
        : 'text/plain';

      return new Response(content, {
        headers: {
          'content-type': contentType,
          'cache-control': 'public, max-age=300',
          'access-control-allow-origin': '*',
        },
      });
    }

    // For all other paths, continue to static assets
    return env.ASSETS.fetch(request);
  },
};
