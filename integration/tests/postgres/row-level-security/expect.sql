alter table documents enable row level security;
alter table documents force row level security;
create policy "tenant_isolation" on documents using (tenant_id = current_setting('app.tenant_id')::integer) with check (tenant_id = current_setting('app.tenant_id')::integer);
create policy "tenant_read" on documents for select to "app_user" using (tenant_id = current_setting('app.tenant_id')::integer);
