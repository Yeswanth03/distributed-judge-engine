-- Sign in with Google or GitHub.

-- Which provider accounts belong to which user. subject is the provider's stable user ID: emails and usernames
-- can change at the provider, this can't.
CREATE TABLE user_identities (
    provider    VARCHAR(16)  NOT NULL,
    subject     TEXT         NOT NULL,
    user_id     UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email       TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX idx_user_identities_user ON user_identities (user_id);

-- A sign-in in progress: the state sent to the provider (hashed), the PKCE verifier, and where to send the
-- browser afterwards. Single use, taken by the callback; minutes long.
CREATE TABLE oauth_states (
    state_hash     BYTEA        PRIMARY KEY,
    provider       VARCHAR(16)  NOT NULL,
    code_verifier  TEXT         NOT NULL,
    redirect_uri   TEXT         NOT NULL,
    expires_at     TIMESTAMPTZ  NOT NULL
);

-- A finished sign-in waiting for the page to collect its tokens. The page gets the code in a redirect and swaps
-- it for tokens with one POST, so tokens never travel in a URL. Single use, seconds long.
CREATE TABLE login_codes (
    code_hash   BYTEA        PRIMARY KEY,
    user_id     UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ  NOT NULL
);
