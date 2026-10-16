# Architecture

Curated by the DM. Keep it short; it is part of every briefing. Move detail into ADRs.

## Modules

- `src/server.ts`: the HTTP server, with its `/health` route.
- `src/main.ts`: starts the server on `PORT`, 3000 by default.
- `e2e/`: end-to-end tests, run against the running app.

## Boundaries

## Data model
