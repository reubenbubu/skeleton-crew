# Conventions

Curated by the DM. Keep it short; it is part of every briefing.

## Code

- Python 3.13. Code lives in the `app` package under `src/`.
- Ruff checks and formats the code. Its defaults apply unless an ADR says otherwise.

## Tests

- Tests use pytest and live under `tests/`, in files named `test_*.py`.
- Every test names the acceptance criterion or risk it covers in its docstring, such as `"""#12-AC1"""`.

## Gotchas
