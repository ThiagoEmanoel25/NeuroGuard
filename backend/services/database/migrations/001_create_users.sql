CREATE TABLE users (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('patient', 'rescuer', 'doctor', 'admin')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized CHECK (email = lower(trim(email))),
    CONSTRAINT users_password_hash_not_empty CHECK (password_hash <> '')
);

CREATE UNIQUE INDEX users_email_unique ON users (lower(email));
