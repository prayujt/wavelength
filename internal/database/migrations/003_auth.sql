CREATE TABLE users (
 id TEXT PRIMARY KEY,
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 name TEXT NOT NULL DEFAULT '',
 email TEXT NOT NULL DEFAULT '',
 UNIQUE(issuer, subject)
);
CREATE TABLE auth_flows (
 token_hash TEXT PRIMARY KEY,
 state_hash TEXT NOT NULL,
 nonce TEXT NOT NULL,
 verifier TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX auth_flows_expiry ON auth_flows(expires_at);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 csrf TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
