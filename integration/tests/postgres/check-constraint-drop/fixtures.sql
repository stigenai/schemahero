create table users (
  id integer primary key not null,
  age integer not null,
  constraint users_age_nonneg check (age >= 0)
);
