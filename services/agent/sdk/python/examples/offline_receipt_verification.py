from layerx_sdk import (
    AuthorizedReceiptBatch,
    LocalSignatureVerifier,
    ReceiptVerification,
    verify_receipt,
)


def offline_receipt_verification(
    canonical_receipt: bytes,
    authorized_batch: AuthorizedReceiptBatch,
    signatures: LocalSignatureVerifier,
) -> ReceiptVerification:
    return verify_receipt(canonical_receipt, authorized_batch, signatures)
