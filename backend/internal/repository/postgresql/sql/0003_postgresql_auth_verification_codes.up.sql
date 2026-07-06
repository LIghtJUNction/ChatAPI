CREATE TABLE IF NOT EXISTS auth_verification_codes (
	email TEXT NOT NULL,
	purpose TEXT NOT NULL,
	code_hash TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY(email, purpose)
);

CREATE INDEX IF NOT EXISTS idx_auth_verification_codes_expires_at
ON auth_verification_codes(expires_at);
