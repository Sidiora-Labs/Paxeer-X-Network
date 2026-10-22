-- Outstanding nonces are resolved by the hash of their signed transaction, so the seeded flag goes.
-- A hold without a hash keeps its created_at and is released once it is older than the broadcast
-- grace period, so a database that applied either form of migration 015 ends in the same schema.
alter table nonce_allocations drop column if exists outstanding_seeded;
