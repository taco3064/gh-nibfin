// The Worker stream the Live page and the Home summary read: the build
// setting WEBSITE_LIVE_URL of astro.config.mjs. Only the islands import it.
export { WEBSITE_LIVE_URL as liveUrl } from 'astro:env/client'
