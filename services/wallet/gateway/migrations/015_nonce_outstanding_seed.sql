alter table nonce_allocations add column if not exists outstanding_seeded boolean not null default false;
