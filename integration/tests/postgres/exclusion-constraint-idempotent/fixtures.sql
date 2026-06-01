-- btree_gist provides the gist operator class for scalar equality (room_id with =).
create extension if not exists btree_gist;

create table reservations (
  id integer primary key not null,
  room_id integer not null,
  during daterange not null,
  constraint reservations_room_during_excl exclude using gist (room_id with =, during with &&) where (room_id is not null)
);
