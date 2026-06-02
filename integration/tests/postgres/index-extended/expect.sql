create table "users" ("id" integer, "email" character varying (255) not null, "phone" character varying (10) not null default '', "created_at" timestamp with time zone not null default now(), "data" jsonb, primary key ("id"));
create unique index idx_users_email_partial on users (email) where phone <> '';
create index idx_users_lower_email on users ((lower(email)));
create index idx_users_phone_hash on users using "hash" (phone);
create index idx_users_data_gin on users using "gin" (data);
create index idx_users_created_id on users (created_at desc, id);
