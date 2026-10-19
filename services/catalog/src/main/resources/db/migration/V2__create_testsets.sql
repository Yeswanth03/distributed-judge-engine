-- Versioned test sets. A version is uploaded as a draft, published once, and never changes after that.

-- The published version the judge uses; NULL until the first publish.
ALTER TABLE problems ADD COLUMN current_version INT;

CREATE TABLE testsets (
    problem_id    UUID        NOT NULL REFERENCES problems (id),
    version       INT         NOT NULL CHECK (version > 0),
    status        TEXT        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    content_hash  TEXT        NOT NULL,
    published_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (problem_id, version)
);

CREATE TABLE test_cases (
    id               UUID    PRIMARY KEY,
    problem_id       UUID    NOT NULL,
    testset_version  INT     NOT NULL,
    ordinal          INT     NOT NULL CHECK (ordinal > 0),
    input            TEXT    NOT NULL,
    expected_output  TEXT    NOT NULL,
    is_sample        BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (problem_id, testset_version) REFERENCES testsets (problem_id, version),
    UNIQUE (problem_id, testset_version, ordinal)
);

-- Immutability is enforced by the database, not just the API, so even direct SQL cannot alter published data.
-- This is what lets the judge cache a test set by (problem, version) forever.

-- Cases: no insert into, update of, or delete from a published set; an update cannot move a case into one either.
CREATE FUNCTION forbid_change_to_published_cases() RETURNS trigger AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND EXISTS (
            SELECT 1 FROM testsets
             WHERE problem_id = OLD.problem_id AND version = OLD.testset_version AND status = 'published') THEN
        RAISE EXCEPTION 'test set % of problem % is published and immutable', OLD.testset_version, OLD.problem_id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') AND EXISTS (
            SELECT 1 FROM testsets
             WHERE problem_id = NEW.problem_id AND version = NEW.testset_version AND status = 'published') THEN
        RAISE EXCEPTION 'test set % of problem % is published and immutable', NEW.testset_version, NEW.problem_id;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER test_cases_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON test_cases
    FOR EACH ROW EXECUTE FUNCTION forbid_change_to_published_cases();

-- Test sets: a published row cannot be changed (including back to draft) or deleted.
CREATE FUNCTION forbid_change_to_published_testset() RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'published' THEN
        RAISE EXCEPTION 'test set % of problem % is published and immutable', OLD.version, OLD.problem_id;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER testsets_immutable
    BEFORE UPDATE OR DELETE ON testsets
    FOR EACH ROW EXECUTE FUNCTION forbid_change_to_published_testset();
