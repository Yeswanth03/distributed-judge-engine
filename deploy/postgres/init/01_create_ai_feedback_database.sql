-- Local development only. Runs after 00_create_databases.sql (which creates catalog and submission).
-- AI Feedback Service: its own database; the service applies its tables from services/ai-service/db/migration.
CREATE DATABASE ai_feedback;
