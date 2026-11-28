/// <reference types="@astrojs/starlight" />
// Starlight route data middleware (registered as routeMiddleware in
// astro.config.mjs). The CLI page renders its command sections through a React
// component, so their <h2>s are not Markdown headings and Starlight's table of
// contents leaves them out. This adds one entry per command, from the same
// committed CLI definition, between Overview and Troubleshooting. The reference
// above brings in Starlight's App.Locals type for starlightRoute.
import { defineRouteMiddleware } from '@astrojs/starlight/route-data'

import commands from '@/app/data/commands.json'
import { tocItemsOf } from '@/app/lib/cli-reference'

export const onRequest = defineRouteMiddleware((context) => {
  const route = context.locals.starlightRoute
  if (route.id !== 'cli' || !route.toc) return
  const items = route.toc.items
  const overview = items[0]?.slug === '_top' ? 1 : 0
  items.splice(overview, 0, ...tocItemsOf(commands))
})
