create table users (
  id integer primary key not null,
  email varchar(255) not null
);

create function audit_fn() returns trigger as $$
begin
  return new;
end;
$$ language plpgsql;
