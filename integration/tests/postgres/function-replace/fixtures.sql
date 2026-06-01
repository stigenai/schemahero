CREATE SCHEMA test;

CREATE TABLE test.users (
  id SERIAL PRIMARY KEY,
  username TEXT NOT NULL,
  email TEXT NOT NULL,
  active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- The function already exists with an OLD body. The spec replaces it with a
-- different body, exercising the "create or replace" (diff-on-body) path.
CREATE FUNCTION test.get_user_count() RETURNS bigint AS $$
BEGIN
    RETURN 0;
END;
$$ language PLpgSQL;
