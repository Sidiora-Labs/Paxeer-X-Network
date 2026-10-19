import type { Duration } from "../../google/protobuf/duration";
export interface ConsensusParams {
    block?: BlockParams;
    evidence?: EvidenceParams;
    validator?: ValidatorParams;
    version?: VersionParams;
    synchrony?: SynchronyParams;
    timeout?: TimeoutParams;
    abci?: ABCIParams;
}
export interface BlockParams {
    /**
     * Max block size, in bytes.
     * Note: must be greater than 0
     */
    max_bytes: number;
    /**
     * Max gas per block.
     * Note: must be greater or equal to -1
     */
    max_gas: number;
}
export interface EvidenceParams {
    /**
     * Max age of evidence, in blocks.
     *
     * The basic formula for calculating this is: MaxAgeDuration / {average block
     * time}.
     */
    max_age_num_blocks: number;
    /**
     * Max age of evidence, in time.
     *
     * It should correspond with an app's "unbonding period" or other similar
     * mechanism for handling [Nothing-At-Stake
     * attacks](https://github.com/ethereum/wiki/wiki/Proof-of-Stake-FAQ#what-is-the-nothing-at-stake-problem-and-how-can-it-be-fixed).
     */
    max_age_duration?: Duration;
    /**
     * This sets the maximum size of total evidence in bytes that can be committed
     * in a single block. and should fall comfortably under the max block bytes.
     * Default is 1048576 or 1MB
     */
    max_bytes: number;
}
export interface ValidatorParams {
    pub_key_types: string[];
}
export interface VersionParams {
    app_version: number;
}
export interface HashedParams {
    block_max_bytes: number;
    block_max_gas: number;
}
export interface SynchronyParams {
    /**
     * message_delay bounds how long a proposal message may take to reach all validators on a network
     * and still be considered valid.
     */
    message_delay?: Duration;
    /**
     * precision bounds how skewed a proposer's clock may be from any validator
     * on the network while still producing valid proposals.
     */
    precision?: Duration;
}
export interface TimeoutParams {
    /**
     * These fields configure the timeouts for the propose step of the Tendermint
     * consensus algorithm: propose is the initial timeout and propose_delta
     * determines how much the timeout grows in subsequent rounds.
     * For the first round, this propose timeout is used and for every subsequent
     * round, the timeout grows by propose_delta.
     *
     * For example:
     * With propose = 10ms, propose_delta = 5ms, the first round's propose phase
     * timeout would be 10ms, the second round's would be 15ms, the third 20ms and so on.
     *
     * If a node waiting for a proposal message does not receive one matching its
     * current height and round before this timeout, the node will issue a
     * nil prevote for the round and advance to the next step.
     */
    propose?: Duration;
    propose_delta?: Duration;
    /**
     * vote along with vote_delta configure the timeout for both of the prevote and
     * precommit steps of the Tendermint consensus algorithm.
     *
     * These parameters influence the vote step timeouts in the the same way that
     * the propose and propose_delta parameters do to the proposal step.
     *
     * The vote timeout does not begin until a quorum of votes has been received. Once
     * a quorum of votes has been seen and this timeout elapses, Tendermint will
     * procced to the next step of the consensus algorithm. If Tendermint receives
     * all of the remaining votes before the end of the timeout, it will proceed
     * to the next step immediately.
     */
    vote?: Duration;
    vote_delta?: Duration;
    /**
     * commit configures how long Tendermint will wait after receiving a quorum of
     * precommits before beginning consensus for the next height. This can be
     * used to allow slow precommits to arrive for inclusion in the next height before progressing.
     */
    commit?: Duration;
    /**
     * bypass_commit_timeout configures the node to proceed immediately to
     * the next height once the node has received all precommits for a block, forgoing
     * the remaining commit timeout.
     * Setting bypass_commit_timeout false (the default) causes Tendermint to wait
     * for the full commit timeout.
     */
    bypass_commit_timeout: boolean;
}
export interface ABCIParams {
    /**
     * vote_extensions_enable_height configures the first height during which
     * vote extensions will be enabled. During this specified height, and for all
     * subsequent heights, precommit messages that do not contain valid extension data
     * will be considered invalid. Prior to this height, vote extensions will not
     * be used or accepted by validators on the network.
     *
     * Once enabled, vote extensions will be created by the application in ExtendVote,
     * passed to the application for validation in VerifyVoteExtension and given
     * to the application to use when proposing a block during PrepareProposal.
     */
    vote_extensions_enable_height: number;
    /**
     * Indicates if CheckTx should be called on all the transactions
     * remaining in the mempool after a block is executed.
     */
    recheck_tx: boolean;
}
