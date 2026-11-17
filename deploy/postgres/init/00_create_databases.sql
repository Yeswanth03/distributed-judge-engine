-- Local development only: one Postgres server, one database per service, each owned by that service's own login.
-- Each service creates its own tables (Catalog with Flyway, Submission with embedded migrations).
-- 03_isolate_databases.sql then stops every other login from connecting to them.

-- Catalog: its own login, owning only its own database.
CREATE USER catalog WITH PASSWORD 'catalog';
CREATE DATABASE catalog OWNER catalog;

-- Submission Service: its own login, owning only its own database.
CREATE USER submission WITH PASSWORD 'submission';
CREATE DATABASE submission OWNER submission;
