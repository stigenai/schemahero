create table reservations (
  id integer primary key not null,
  during daterange not null,
  constraint reservations_during_excl exclude using gist (during with &&)
);
