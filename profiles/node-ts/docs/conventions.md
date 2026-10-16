# Conventions

Curated by the DM. Keep it short; it is part of every briefing.

## Code

- TypeScript in ES modules, run by Node.js 24 directly, without a build step. `tsc` only type-checks.
- Use only TypeScript syntax that Node.js can strip: no `enum`, no `namespace`, no parameter properties. Import local files with their `.ts` extension, and types with `import type`.
- ESLint and Prettier check the code. Their defaults apply unless an ADR says otherwise.

## Tests

- Tests use `node:test` and `node:assert/strict`. Unit tests sit next to the code, in files named `*.test.ts`, or under `test/`.
- End-to-end tests live in `e2e/`, in files named `*.e2e.ts`, and call the running app at `BASE_URL`, `http://localhost:3000` by default.
- Every test names the acceptance criterion or risk it covers in its name, such as `test("#12-AC1: rejects an unknown email", ...)`.

## Gotchas

- Dependencies are installed in the image. After changing `package.json`, run `npm install` in the container, with `crew run setup`, so `package-lock.json` follows.
