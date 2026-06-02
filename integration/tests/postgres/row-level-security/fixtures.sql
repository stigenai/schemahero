-- The table pre-exists (RLS OFF, no policies) so the plan exercises the
-- existing-table diff path: ENABLE + FORCE toggles and CREATE POLICY for each
-- declared policy. The app_user role must pre-exist or CREATE POLICY ... TO
-- app_user fails.
create role app_user;

create table documents (
  id integer primary key not null,
  tenant_id integer not null,
  body text
);
