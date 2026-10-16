# 0001 Node.js and TypeScript stack

Summary: TypeScript on Node.js 24 in containers, run without a build step, tested with node:test and checked with tsc, ESLint and Prettier.

## Decision

- The application runs in the `app` service of `compose.yaml`, built from `Containerfile` on `node:24-slim`, and listens on port 3000.
- Node.js runs the `.ts` files directly by stripping their types. `tsc` type-checks them and emits nothing.
- Dependencies are declared in `package.json` and locked in `package-lock.json`.
- `node:test` runs the tests. ESLint with typescript-eslint lints, and Prettier formats.

## Why

These tools come with Node.js or are the most widely used for each job, and none needs a bundler or a compile step.
