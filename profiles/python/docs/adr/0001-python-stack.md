# 0001 Python stack

Summary: Python 3.13 in containers, packaged with pyproject.toml, tested with pytest and checked with ruff.

## Decision

- The application runs in the `app` service of `compose.yaml`, built from `Containerfile` on `python:3.13-slim`.
- Dependencies are declared in `pyproject.toml`. Development tools are in the `dev` extra.
- pytest runs the tests. Ruff lints and formats.

## Why

These are the most widely used tools for each job, and each needs no configuration to start.
