delete from nonce_outstanding where tx_hash is null;
alter table nonce_allocations drop column if exists outstanding_seeded;
