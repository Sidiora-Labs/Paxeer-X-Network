CREATE TABLE perps.auth_challenges (
    nonce bytea PRIMARY KEY CHECK (octet_length(nonce) = 32),
    address bytea NOT NULL CHECK (octet_length(address) = 20),
    message text NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > issued_at),
    used_at timestamptz
);
CREATE INDEX auth_challenges_address_issued_at_idx ON perps.auth_challenges (address, issued_at);

CREATE TABLE perps.user_alerts (
    id bigserial PRIMARY KEY,
    address bytea NOT NULL CHECK (octet_length(address) = 20),
    alert_type text NOT NULL CHECK (length(btrim(alert_type)) > 0),
    market_id perps.uint256,
    threshold numeric CHECK (threshold NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)),
    enabled boolean NOT NULL DEFAULT true,
    triggered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_alerts_address_idx ON perps.user_alerts (address);

CREATE TABLE perps.referrals (
    address bytea PRIMARY KEY CHECK (octet_length(address) = 20),
    referral_code text UNIQUE CHECK (length(referral_code) > 0),
    referred_by bytea REFERENCES perps.referrals(address) CHECK (octet_length(referred_by) = 20 AND referred_by <> address),
    applied_at timestamptz,
    total_earned perps.uint256 NOT NULL DEFAULT 0,
    CHECK ((referred_by IS NULL) = (applied_at IS NULL))
);
CREATE INDEX referrals_referred_by_idx ON perps.referrals (referred_by);

CREATE TABLE perps.referral_tiers (
    tier text PRIMARY KEY,
    min_referrals integer NOT NULL CHECK (min_referrals >= 0),
    min_volume perps.uint256 NOT NULL,
    reward_bps integer NOT NULL CHECK (reward_bps BETWEEN 0 AND 10000),
    fee_discount_bps integer NOT NULL CHECK (fee_discount_bps BETWEEN 0 AND 10000)
);

CREATE TABLE perps.competitions (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    description text,
    metric text NOT NULL CHECK (metric IN ('pnl', 'volume')),
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL CHECK (end_at > start_at),
    prize_pool perps.uint256 NOT NULL,
    status text NOT NULL
);
CREATE INDEX competitions_status_start_at_idx ON perps.competitions (status, start_at);

CREATE TABLE perps.competition_entries (
    competition_id bigint NOT NULL REFERENCES perps.competitions(id),
    address bytea NOT NULL CHECK (octet_length(address) = 20),
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (competition_id, address)
);
