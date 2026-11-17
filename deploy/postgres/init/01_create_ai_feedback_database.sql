-- Local development only. Runs after 00_create_databases.sql (which creates catalog and submission).
-- AI Feedback Service: its own login, owning only its own database; the service applies its tables from
-- services/ai-service/db/migration.
CREATE USER ai_feedback WITH PASSWORD 'ai_feedback';
CREATE DATABASE ai_feedback OWNER ai_feedback;
