package types

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const (
	BudgetCreatePayloadBytes        = 211
	BudgetCreateNativePayloadBytes  = 251
	BudgetFundPayloadBytes          = 50
	BudgetFundNativePayloadBytes    = 58
	BudgetAmendPayloadBytes         = 75
	BudgetDelegatePayloadBytes      = 66
	BudgetSpendPayloadBytes         = 82
	BudgetClosePayloadBytes         = 42
	BudgetDefundPayloadBytes        = 50
	BudgetRevokePayloadBytes        = 42
	BudgetDelegateSpendPayloadBytes = 114
	PayerGrantBytes                 = 346
	BudgetPullPayloadBytes          = BudgetDelegateSpendPayloadBytes + PayerGrantBytes

	payerGrantRecurringOffset = 160
	payerGrantReferenceOffset = 209
)

var (
	errBudgetNonCanonical  = errors.New("LXP_NON_CANONICAL")
	errBudgetInvalidAmount = errors.New("LXP_INVALID_AMOUNT")
)

func EncodeBudgetCreate(budgetID, budgetAccount, assetID, purposeHash [32]byte, perPeriodLimit, carryCap, amount codec.U128, periodLength, periodStart, expiry, revocationSequence uint64, rolloverPolicy uint8) ([]byte, error) {
	if err := budgetCreateCheck(budgetID, budgetAccount, assetID, perPeriodLimit, amount); err != nil {
		return nil, err
	}
	payload := budgetPayload(BudgetCreatePayloadBytes, 1)
	budgetCreateBody(payload, budgetID, budgetAccount, assetID, purposeHash, perPeriodLimit, carryCap, amount, periodLength, periodStart, expiry, revocationSequence, rolloverPolicy)
	return payload, nil
}

func EncodeBudgetCreateNative(budgetID, budgetAccount, assetID, purposeHash [32]byte, perPeriodLimit, carryCap, amount codec.U128, periodLength, periodStart, expiry, revocationSequence uint64, rolloverPolicy uint8, sourceAccount [32]byte, sourceSequence uint64) ([]byte, error) {
	if !budgetNonzero(sourceAccount) || sourceSequence == math.MaxUint64 {
		return nil, errBudgetNonCanonical
	}
	if err := budgetCreateCheck(budgetID, budgetAccount, assetID, perPeriodLimit, amount); err != nil {
		return nil, err
	}
	payload := budgetPayload(BudgetCreateNativePayloadBytes, 2)
	budgetCreateBody(payload, budgetID, budgetAccount, assetID, purposeHash, perPeriodLimit, carryCap, amount, periodLength, periodStart, expiry, revocationSequence, rolloverPolicy)
	copy(payload[211:243], sourceAccount[:])
	binary.BigEndian.PutUint64(payload[243:251], sourceSequence)
	return payload, nil
}

func EncodeBudgetFund(budgetID [32]byte, amount codec.U128) ([]byte, error) {
	if err := budgetAmountCheck(amount, budgetID); err != nil {
		return nil, err
	}
	return budgetIDAmount(BudgetFundPayloadBytes, 1, budgetID, amount), nil
}

func EncodeBudgetFundNative(budgetID [32]byte, amount codec.U128, sourceSequence uint64) ([]byte, error) {
	if sourceSequence == math.MaxUint64 {
		return nil, errBudgetNonCanonical
	}
	if err := budgetAmountCheck(amount, budgetID); err != nil {
		return nil, err
	}
	payload := budgetIDAmount(BudgetFundNativePayloadBytes, 2, budgetID, amount)
	binary.BigEndian.PutUint64(payload[50:58], sourceSequence)
	return payload, nil
}

func EncodeBudgetAmend(budgetID [32]byte, perPeriodLimit, carryCap codec.U128, expiry uint64, rolloverPolicy uint8) ([]byte, error) {
	if err := budgetAmountCheck(perPeriodLimit, budgetID); err != nil {
		return nil, err
	}
	payload := budgetIDAmount(BudgetAmendPayloadBytes, 1, budgetID, perPeriodLimit)
	carry := carryCap.Bytes()
	copy(payload[50:66], carry[:])
	binary.BigEndian.PutUint64(payload[66:74], expiry)
	payload[74] = rolloverPolicy
	return payload, nil
}

func EncodeBudgetDelegate(budgetID, delegate [32]byte) ([]byte, error) {
	if !budgetNonzero(budgetID, delegate) {
		return nil, errBudgetNonCanonical
	}
	payload := budgetPayload(BudgetDelegatePayloadBytes, 1)
	copy(payload[2:34], budgetID[:])
	copy(payload[34:66], delegate[:])
	return payload, nil
}

func EncodeBudgetSpend(budgetID, recipient [32]byte, amount codec.U128) ([]byte, error) {
	if err := budgetAmountCheck(amount, budgetID, recipient); err != nil {
		return nil, err
	}
	payload := budgetPayload(BudgetSpendPayloadBytes, 1)
	budgetSpendBody(payload, budgetID, recipient, amount)
	return payload, nil
}

func EncodeBudgetClose(budgetID [32]byte, revocationSequence uint64) ([]byte, error) {
	return budgetIDSequence(BudgetClosePayloadBytes, budgetID, revocationSequence)
}

func EncodeBudgetDefund(budgetID [32]byte, amount codec.U128) ([]byte, error) {
	if err := budgetAmountCheck(amount, budgetID); err != nil {
		return nil, err
	}
	return budgetIDAmount(BudgetDefundPayloadBytes, 1, budgetID, amount), nil
}

func EncodeBudgetRevoke(budgetID [32]byte, revocationSequence uint64) ([]byte, error) {
	return budgetIDSequence(BudgetRevokePayloadBytes, budgetID, revocationSequence)
}

func EncodeBudgetDelegateSpend(budgetID, recipient [32]byte, amount codec.U128, idempotencyKey [32]byte) ([]byte, error) {
	if err := budgetAmountCheck(amount, budgetID, recipient, idempotencyKey); err != nil {
		return nil, err
	}
	payload := budgetPayload(BudgetDelegateSpendPayloadBytes, 1)
	budgetSpendBody(payload, budgetID, recipient, amount)
	copy(payload[82:114], idempotencyKey[:])
	return payload, nil
}

func EncodeBudgetPull(budgetID, recipient [32]byte, amount codec.U128, idempotencyKey [32]byte, payerGrant []byte) ([]byte, error) {
	if err := budgetAmountCheck(amount, budgetID, recipient, idempotencyKey); err != nil {
		return nil, err
	}
	if len(payerGrant) != PayerGrantBytes || payerGrant[payerGrantRecurringOffset] > 1 || payerGrant[payerGrantReferenceOffset] > 1 {
		return nil, errBudgetNonCanonical
	}
	payload := budgetPayload(BudgetPullPayloadBytes, 1)
	budgetSpendBody(payload, budgetID, recipient, amount)
	copy(payload[82:114], idempotencyKey[:])
	copy(payload[BudgetDelegateSpendPayloadBytes:], payerGrant)
	return payload, nil
}

func budgetPayload(length int, version byte) []byte {
	payload := make([]byte, length)
	payload[1] = version
	return payload
}

func budgetNonzero(values ...[32]byte) bool {
	for _, value := range values {
		if value == ([32]byte{}) {
			return false
		}
	}
	return true
}

func budgetCreateCheck(budgetID, budgetAccount, assetID [32]byte, perPeriodLimit, amount codec.U128) error {
	if !budgetNonzero(budgetID, budgetAccount, assetID) {
		return errBudgetNonCanonical
	}
	if amount.IsZero() || perPeriodLimit.IsZero() {
		return errBudgetInvalidAmount
	}
	return nil
}

func budgetCreateBody(payload []byte, budgetID, budgetAccount, assetID, purposeHash [32]byte, perPeriodLimit, carryCap, amount codec.U128, periodLength, periodStart, expiry, revocationSequence uint64, rolloverPolicy uint8) {
	copy(payload[2:34], budgetID[:])
	copy(payload[34:66], budgetAccount[:])
	copy(payload[66:98], assetID[:])
	copy(payload[98:130], purposeHash[:])
	limit := perPeriodLimit.Bytes()
	copy(payload[130:146], limit[:])
	carry := carryCap.Bytes()
	copy(payload[146:162], carry[:])
	value := amount.Bytes()
	copy(payload[162:178], value[:])
	binary.BigEndian.PutUint64(payload[178:186], periodLength)
	binary.BigEndian.PutUint64(payload[186:194], periodStart)
	binary.BigEndian.PutUint64(payload[194:202], expiry)
	binary.BigEndian.PutUint64(payload[202:210], revocationSequence)
	payload[210] = rolloverPolicy
}

func budgetAmountCheck(amount codec.U128, ids ...[32]byte) error {
	if !budgetNonzero(ids...) {
		return errBudgetNonCanonical
	}
	if amount.IsZero() {
		return errBudgetInvalidAmount
	}
	return nil
}

func budgetIDAmount(length int, version byte, budgetID [32]byte, amount codec.U128) []byte {
	payload := budgetPayload(length, version)
	copy(payload[2:34], budgetID[:])
	value := amount.Bytes()
	copy(payload[34:50], value[:])
	return payload
}

func budgetIDSequence(length int, budgetID [32]byte, revocationSequence uint64) ([]byte, error) {
	if !budgetNonzero(budgetID) || revocationSequence == 0 {
		return nil, errBudgetNonCanonical
	}
	payload := budgetPayload(length, 1)
	copy(payload[2:34], budgetID[:])
	binary.BigEndian.PutUint64(payload[34:42], revocationSequence)
	return payload, nil
}

func budgetSpendBody(payload []byte, budgetID, recipient [32]byte, amount codec.U128) {
	copy(payload[2:34], budgetID[:])
	copy(payload[34:66], recipient[:])
	value := amount.Bytes()
	copy(payload[66:82], value[:])
}
