-- Problems become functions to implement: the judge calls function_name with each case's JSON arguments.
-- Nullable so existing problems survive; the API refuses test sets for a problem until all four are set.

ALTER TABLE problems
    ADD COLUMN function_name TEXT,
    ADD COLUMN parameters    JSONB,
    ADD COLUMN return_type   TEXT,
    ADD COLUMN starter_code  TEXT,
    ADD CONSTRAINT function_details_all_or_none CHECK (
        num_nonnulls(function_name, parameters, return_type, starter_code) IN (0, 4));
