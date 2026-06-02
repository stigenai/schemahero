-- The table is created already RLS-enabled + forced, with policies that match
-- EXACTLY what the spec declares. The USING/WITH CHECK expressions are written in
-- the SAME raw form the spec uses; PostgreSQL re-renders them canonically in
-- pg_get_expr (adds parens, ::text casts), so this fixture also proves the diff
-- does NOT churn on that canonicalization: the plan must be EMPTY (expect.sql is
-- empty). If the policy-expression comparison were naive string equality, every
-- plan would emit a drop+recreate and this test would FAIL — that is the point.
create role app_user;

create table documents (
  id integer primary key not null,
  tenant_id integer not null,
  body text
);

alter table documents enable row level security;
alter table documents force row level security;

create policy "tenant_isolation" on documents
  using (tenant_id = current_setting('app.tenant_id')::integer)
  with check (tenant_id = current_setting('app.tenant_id')::integer);

create policy "tenant_read" on documents
  for select
  to app_user
  using (tenant_id = current_setting('app.tenant_id')::integer);
