# Conventions

Curated by the DM. Keep it short; it is part of every briefing.

## Code

- Java 25 and Spring Boot, built with Maven. Code lives in the `app` package.
- Spotless formats the code with google-java-format. Run `crew run lint` to check it; `mvn spotless:apply` fixes it.
- The app reaches the database at `db:5432` and listens on port 8080. `/actuator/health` reports its health, including the database's.

## Database migrations

- Every schema change is a Flyway migration in `src/main/resources/db/migration/`, named `V<UTC time as yyyyMMddHHmmss>__<what_it_does>.sql`, such as `V20261016091200__create_users.sql`. Take the time when you write the migration.
- Changes go forward only. A migration that has reached `main` is never edited, renamed or deleted; fix it with a new migration.
- Time-based versions do not clash, so a rebase never needs to renumber a migration. `spring.flyway.out-of-order` is on, so a migration merged after one with a later version still applies.
- Migrations from different tickets must not depend on each other's order unless one ticket depends on the other.
- Every gate run starts from a fresh database and applies every migration from the start.

## Tests

- Tests use JUnit 5 and live under `src/test/java/`, in classes named `*Test` or `*Tests`. Tests that need the app's context use `@SpringBootTest`; they run against the real database.
- End-to-end tests are classes named `*E2E`, which `test` leaves out. They call the running app at `BASE_URL`, `http://localhost:8080` by default.
- Every test names the acceptance criterion or risk it covers with `@DisplayName`, such as `@DisplayName("#12-AC1: rejects an unknown email")`.

## Gotchas
