-- Coordinator renders __SCHEMA__ only after validating [a-z][a-z0-9_]{1,62}.
-- Rendered into the recovery-owned migration by the serial coordinator.
CREATE TABLE __SCHEMA__.recovery_control (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 epoch bigint NOT NULL CHECK(epoch > 0), region text NOT NULL CHECK(region ~ '^[a-z][a-z0-9-]{1,62}$'),
 revision bigint NOT NULL CHECK(revision > 0),
 checkpoint_sha256 text NOT NULL CHECK(checkpoint_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE TABLE __SCHEMA__.recovery_proposals (
 proposal_id text PRIMARY KEY CHECK(proposal_id ~ '^[a-z][a-z0-9-]{1,62}$'), expected_epoch bigint NOT NULL CHECK(expected_epoch > 0),
 next_region text NOT NULL CHECK(next_region ~ '^[a-z][a-z0-9-]{1,62}$'), revision bigint NOT NULL CHECK(revision > 0),
 checkpoint_sha256 text NOT NULL CHECK(checkpoint_sha256 ~ '^[0-9a-f]{64}$'),
 fence_proof_sha256 text CHECK(fence_proof_sha256 ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('proposed','validated','activated','rejected')),
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 128), created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE __SCHEMA__.recovery_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 proposal_id text NOT NULL, state text NOT NULL, actor text NOT NULL,
 revision bigint NOT NULL, epoch bigint NOT NULL, region text NOT NULL,
 checkpoint_sha256 text NOT NULL, fence_proof_sha256 text,
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE __SCHEMA__.recovery_broker_fences (
 proposal_id text PRIMARY KEY REFERENCES __SCHEMA__.recovery_proposals(proposal_id),
 expected_epoch bigint NOT NULL CHECK(expected_epoch>0),
 region text NOT NULL CHECK(region ~ '^[a-z][a-z0-9-]{1,62}$'),
 revision bigint NOT NULL CHECK(revision>0),
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 128),
 configuration_sha256 text NOT NULL CHECK(configuration_sha256 ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('applying','verified','failed')),
 proof_sha256 text CHECK(proof_sha256 ~ '^[0-9a-f]{64}$'),
 evidence jsonb,
 failure_code text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 verified_at timestamptz,
 CHECK(state<>'verified' OR (proof_sha256 IS NOT NULL AND evidence IS NOT NULL AND verified_at IS NOT NULL))
);
-- Called in SAME transaction as every authoritative write or durable inbox effect.
-- FOR SHARE blocks cutover until the guarded transaction commits/rolls back.
CREATE FUNCTION __SCHEMA__.assert_epoch(wanted bigint, wanted_region text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE current_epoch bigint; current_region text;
BEGIN
 SELECT epoch,region INTO current_epoch,current_region FROM __SCHEMA__.recovery_control WHERE singleton FOR SHARE;
 IF NOT FOUND OR wanted IS DISTINCT FROM current_epoch OR wanted_region IS DISTINCT FROM current_region THEN
  RAISE EXCEPTION 'stale restoration epoch or region' USING ERRCODE='55000';
 END IF;
END $$;
CREATE FUNCTION __SCHEMA__.current_epoch() RETURNS TABLE(epoch bigint,region text)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
 SELECT epoch,region FROM __SCHEMA__.recovery_control WHERE singleton
$$;
-- Coordinator-only. Digest proves a pinned external fence record exists, NOT its validity.
-- The orchestrator must independently verify provider/network/credential revocations.
CREATE FUNCTION __SCHEMA__.activate(proposal text, authenticated_actor text) RETURNS bigint
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE c __SCHEMA__.recovery_control%ROWTYPE; p __SCHEMA__.recovery_proposals%ROWTYPE;
BEGIN
 SELECT * INTO c FROM __SCHEMA__.recovery_control WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'missing restoration authority'; END IF;
 SELECT * INTO p FROM __SCHEMA__.recovery_proposals WHERE proposal_id=proposal FOR UPDATE;
 IF NOT FOUND OR p.state <> 'validated' OR p.fence_proof_sha256 IS NULL OR
    p.actor IS DISTINCT FROM authenticated_actor OR length(authenticated_actor)=0 OR
    p.expected_epoch <> c.epoch OR p.revision <= c.revision OR p.next_region = c.region THEN
  RAISE EXCEPTION 'invalid recovery activation' USING ERRCODE='55000';
 END IF;
 UPDATE __SCHEMA__.recovery_control SET epoch=c.epoch+1,region=p.next_region,revision=p.revision,
   checkpoint_sha256=p.checkpoint_sha256 WHERE singleton;
 UPDATE __SCHEMA__.recovery_proposals SET state='activated' WHERE proposal_id=proposal;
 INSERT INTO __SCHEMA__.recovery_audit(proposal_id,state,actor,revision,epoch,region,checkpoint_sha256,fence_proof_sha256)
 VALUES(proposal,'activated',authenticated_actor,p.revision,c.epoch+1,p.next_region,p.checkpoint_sha256,p.fence_proof_sha256);
 RETURN c.epoch+1;
END $$;
CREATE FUNCTION __SCHEMA__.activate_fenced_v2(proposal text, authenticated_actor text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE c __SCHEMA__.recovery_control%ROWTYPE; p __SCHEMA__.recovery_proposals%ROWTYPE;
BEGIN
 SELECT * INTO c FROM __SCHEMA__.recovery_control WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'missing restoration authority' USING ERRCODE='55000'; END IF;
 SELECT * INTO p FROM __SCHEMA__.recovery_proposals WHERE proposal_id=proposal FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM __SCHEMA__.recovery_broker_fences f
     WHERE f.proposal_id=p.proposal_id AND f.expected_epoch=c.epoch AND f.region=c.region
      AND f.revision=p.revision AND f.actor=authenticated_actor AND f.state='verified'
      AND f.proof_sha256=p.fence_proof_sha256
      AND f.verified_at >= clock_timestamp()-interval '5 minutes') THEN
  RAISE EXCEPTION 'actual broker epoch fence required' USING ERRCODE='55000';
 END IF;
 RETURN __SCHEMA__.activate(proposal,authenticated_actor);
END $$;
REVOKE ALL ON ALL TABLES IN SCHEMA __SCHEMA__ FROM PUBLIC;
REVOKE ALL ON FUNCTION __SCHEMA__.activate(text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION __SCHEMA__.assert_epoch(bigint,text) FROM PUBLIC;

REVOKE ALL ON FUNCTION __SCHEMA__.current_epoch() FROM PUBLIC;

REVOKE ALL ON FUNCTION __SCHEMA__.activate_fenced_v2(text,text) FROM PUBLIC;

CREATE TABLE __SCHEMA__.recovery_validations_v3 (
 proposal_id text PRIMARY KEY REFERENCES __SCHEMA__.recovery_proposals(proposal_id),
 observation_sha256 text NOT NULL CHECK(observation_sha256 ~ '^[0-9a-f]{64}$' AND observation_sha256<>repeat('0',64)),
 scenarios_sha256 text NOT NULL CHECK(scenarios_sha256 ~ '^[0-9a-f]{64}$' AND scenarios_sha256<>repeat('0',64)),
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 128),
 validated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 valid_until timestamptz NOT NULL CHECK(valid_until>validated_at)
);
CREATE FUNCTION __SCHEMA__.activate_fenced_v3(proposal text, observation_digest text, scenarios_digest text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p __SCHEMA__.recovery_proposals%ROWTYPE; v __SCHEMA__.recovery_validations_v3%ROWTYPE;
BEGIN
 -- Match the existing activation lock order, including concurrent v2 callers.
 PERFORM 1 FROM __SCHEMA__.recovery_control WHERE singleton FOR UPDATE;
 SELECT * INTO p FROM __SCHEMA__.recovery_proposals WHERE proposal_id=proposal FOR UPDATE;
 IF NOT FOUND OR p.actor IS DISTINCT FROM session_user::text THEN
  RAISE EXCEPTION 'authenticated recovery principal required' USING ERRCODE='42501';
 END IF;
 SELECT * INTO v FROM __SCHEMA__.recovery_validations_v3 WHERE proposal_id=proposal FOR UPDATE;
 IF NOT FOUND OR v.actor IS DISTINCT FROM session_user::text OR
    v.observation_sha256 IS DISTINCT FROM observation_digest OR
    v.scenarios_sha256 IS DISTINCT FROM scenarios_digest OR
    v.valid_until<=clock_timestamp() OR v.validated_at>clock_timestamp() THEN
  RAISE EXCEPTION 'fresh exact recovery validation required' USING ERRCODE='55000';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM __SCHEMA__.recovery_broker_fences f WHERE f.proposal_id=proposal
    AND f.state='verified' AND f.verified_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'future or missing broker fence observation' USING ERRCODE='55000';
 END IF;
 RETURN __SCHEMA__.activate_fenced_v2(proposal,session_user::text);
END $$;
REVOKE ALL ON __SCHEMA__.recovery_validations_v3 FROM PUBLIC;
REVOKE ALL ON FUNCTION __SCHEMA__.activate_fenced_v3(text,text,text) FROM PUBLIC;
