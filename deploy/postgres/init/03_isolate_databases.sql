-- Local development only. Runs last.
-- By default any login may connect to any database. Take that away, so each service's login reaches only the
-- database it owns (owners keep their access; the postgres superuser keeps everything for admin work).
REVOKE CONNECT ON DATABASE catalog FROM PUBLIC;
REVOKE CONNECT ON DATABASE submission FROM PUBLIC;
REVOKE CONNECT ON DATABASE users FROM PUBLIC;
REVOKE CONNECT ON DATABASE ai_feedback FROM PUBLIC;

-- Monitoring (postgres-exporter): pg_monitor reads statistics and sizes for every database, but no table data.
CREATE USER exporter WITH PASSWORD 'exporter';
GRANT pg_monitor TO exporter;
