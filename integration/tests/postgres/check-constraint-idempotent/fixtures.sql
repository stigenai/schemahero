create table users (
  id integer primary key not null,
  age integer not null,
  score integer not null,
  constraint users_age_nonneg check (age >= 0),
  constraint users_score_max check (score <= 100)
);
