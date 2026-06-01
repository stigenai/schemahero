create table users (
  id integer primary key not null,
  email varchar(255) not null,
  phone varchar(10) not null default '',
  created_at timestamptz not null default now(),
  data jsonb
);

-- Partial unique index. The predicate is written in PostgreSQL's canonical
-- form (matching pg_get_expr output) so the planner sees no change.
create unique index idx_users_email_partial on users (email) where ((phone)::text <> ''::text);

-- Functional / expression index, canonical form (matches pg_get_indexdef).
create index idx_users_lower_email on users (lower((email)::text));

-- Index method hash (scalar column).
create index idx_users_phone_hash on users using hash (phone);

-- Index method gin on a jsonb column.
create index idx_users_data_gin on users using gin (data);

-- Per-column ordering (DESC + default ASC).
create index idx_users_created_id on users (created_at desc, id);
