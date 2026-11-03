-- Local development only: one Postgres server, one database per service.
-- Each service creates its own tables (Catalog with Flyway, Submission with embedded migrations).

-- Catalog: matches the defaults in services/catalog application-local.properties (user postgres).
CREATE DATABASE catalog;

-- Submission Service: its own login, owning only its own database.
CREATE USER submission WITH PASSWORD 'submission';
CREATE DATABASE submission OWNER submission;
