//! Balance reads delegated to the proof-verifying LNI client.

use layerx_client::lni::transport::FrameTransport;
use layerx_client::read::{self, ReadContext, ReadError};
use layerx_types::amount::Amount;
use layerx_types::verify::VerificationLevel;

/// Every freshness coordinate required to judge a returned balance.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Freshness {
    pub value_global_sequence: u64,
    pub value_batch_number: u64,
    pub observed_head_sequence: u64,
    pub latest_sealed_batch: u64,
    pub latest_finalised_checkpoint: [u8; 32],
}

/// Core-produced balance bytes with their locally achieved evidence level.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BalanceRead {
    pub account: [u8; 32],
    pub asset: [u8; 32],
    pub amount: Amount,
    pub canonical_bytes: Vec<u8>,
    pub achieved: VerificationLevel,
    pub freshness: Freshness,
}

/// Serves a balance only when the LNI response locally proves the requested level.
///
/// # Errors
///
/// Returns the client read failure when the transport fails, the response does not match the
/// request, or the response cannot locally prove the requested verification level.
pub fn balance(
    transport: &mut dyn FrameTransport,
    account: [u8; 32],
    asset: [u8; 32],
    context: ReadContext,
) -> Result<BalanceRead, ReadError> {
    let latest_sealed_batch = context.head.sealed_batch;
    let value = read::balance(transport, account, asset, context)?;
    let freshness = value.freshness();
    Ok(BalanceRead {
        account: value.account,
        asset: value.asset,
        amount: value.amount,
        canonical_bytes: value.canonical_bytes().to_vec(),
        achieved: value.achieved(),
        freshness: Freshness {
            value_global_sequence: freshness.global_sequence,
            value_batch_number: freshness.batch_number,
            observed_head_sequence: freshness.observed_head_sequence,
            latest_sealed_batch,
            latest_finalised_checkpoint: freshness.observed_checkpoint,
        },
    })
}
