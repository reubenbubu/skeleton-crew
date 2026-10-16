# Architecture

Curated by the DM. Keep it short; it is part of every briefing. Move detail into ADRs.

## Modules

- `src/main/java/app/`: the Spring Boot application, in the `app` package.
- `src/main/resources/db/migration/`: the Flyway migrations.
- `src/test/java/app/`: the tests.

## Boundaries

## Data model

PostgreSQL, in the `db` service of `compose.yaml`. The schema is the sum of the migrations.
