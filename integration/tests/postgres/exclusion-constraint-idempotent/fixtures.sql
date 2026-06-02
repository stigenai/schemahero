-- btree_gist provides the gist operator class for scalar equality (room_id with =).
create extension if not exists btree_gist;

create table reservations (
  id integer primary key not null,
  room_id integer not null,
  during daterange not null,
  code varchar(10) not null default '',
  -- The WHERE predicate is created in NATURAL form here; PostgreSQL stores and
  -- renders it back canonically as "((code)::text <> ''::text)". The idempotent
  -- re-plan against the natural-form spec must produce no DDL (HIGH-2 proof).
  constraint reservations_room_during_excl exclude using gist (room_id with =, during with &&) where (code <> '')
);
