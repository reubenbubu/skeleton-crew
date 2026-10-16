# 0001 Java and Spring Boot stack

Summary: Java 25 and Spring Boot in containers, built and tested with Maven, formatted with Spotless, with PostgreSQL and Flyway migrations.

## Decision

- The application runs in the `app` service of `compose.yaml`, built from `Containerfile` on `maven:3.9-eclipse-temurin-25`. PostgreSQL 18 runs in the `db` service.
- Dependencies are declared in `pom.xml`, whose parent is `spring-boot-starter-parent`.
- JUnit 5 runs the tests through Maven Surefire. Spotless with google-java-format checks the formatting.
- Flyway applies the migrations in `src/main/resources/db/migration/` when the app starts. Versions are UTC times, and migrations go forward only.

## Why

These are the most widely used tools for each job in the Spring ecosystem, and Spring Boot configures them with no setup.
