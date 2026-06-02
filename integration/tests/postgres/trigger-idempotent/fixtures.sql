create table users (
  id integer primary key not null,
  email varchar(255) not null
);

create function audit_fn() returns trigger as $$
begin
  return new;
end;
$$ language plpgsql;

-- Deliberately created with the legacy EXECUTE PROCEDURE spelling; pg_get_triggerdef
-- reports it as EXECUTE FUNCTION on PG12+. The plan must still treat the spec
-- (type: Function) as a no-op, proving the procedure/function + unquoted-identifier
-- normalization holds against real catalog output.
create trigger audit_users
  after insert or update on users
  for each row
  execute procedure audit_fn();
