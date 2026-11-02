import type { APIRoute } from 'astro';
import { getAvailableLocales } from '../lib/content';

/** The published build's locale list; an open page polls this after translations finish. */
export const GET: APIRoute = async () => new Response(
  JSON.stringify({ locales: await getAvailableLocales() }),
  { headers: { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' } }
);
