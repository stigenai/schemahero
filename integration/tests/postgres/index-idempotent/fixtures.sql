create table users (
  id integer primary key not null,
  email varchar(255) not null,
  phone varchar(10) not null default '',
  created_at timestamptz not null default now(),
  data jsonb
);

-- Partial unique index. The predicate is created in NATURAL form; PostgreSQL
-- stores and renders it back canonically as "((phone)::text <> ''::text)". The
-- natural-form spec must re-plan to no DDL (HIGH-2 proof).
create unique index idx_users_email_partial on users (email) where phone <> '';

-- Functional / expression index, also created in natural form (PostgreSQL stores
-- it as "lower((email)::text)").
create index idx_users_lower_email on users (lower(email));

-- Index method hash (scalar column).
create index idx_users_phone_hash on users using hash (phone);

-- Index method gin on a jsonb column.
create index idx_users_data_gin on users using gin (data);

-- Per-column ordering (DESC + default ASC).
create index idx_users_created_id on users (created_at desc, id);
