create trigger "audit_users" after insert or update on "users" for each row execute function audit_fn();
