create table if not exists wallet_lx_approvals (
  id uuid primary key,
  wallet_id uuid not null references wallets(id),
  principal uuid not null,
  session_id uuid not null unique,
  state text not null check (state in ('reviewed','approved','signing_unknown','signed')),
  artifact jsonb not null check (jsonb_typeof(artifact) = 'object' and octet_length(artifact::text) <= 2400000),
  review_evidence jsonb not null check (jsonb_typeof(review_evidence) = 'array' and jsonb_array_length(review_evidence) = 3 and octet_length(review_evidence::text) <= 32768),
  approval jsonb check (approval is null or (jsonb_typeof(approval) = 'object' and octet_length(approval::text) <= 4096)),
  evidence jsonb check (evidence is null or (jsonb_typeof(evidence) = 'object' and octet_length(evidence::text) <= 32768)),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  approved_at timestamptz,
  attempted_at timestamptz,
  completed_at timestamptz,
  check ((state = 'reviewed') = (approval is null)),
  check ((state in ('signing_unknown','signed')) = (attempted_at is not null)),
  check ((state = 'signed') = (evidence is not null)),
  check ((state = 'signed') = (completed_at is not null))
);
create index if not exists wallet_lx_approvals_owner on wallet_lx_approvals(principal,created_at);
