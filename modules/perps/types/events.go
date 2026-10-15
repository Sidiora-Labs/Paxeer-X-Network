package types

// Event names of contracts/perps.md section 18, each the name of an event of
// precompiles/perps/abi/abi.json.
const (
	EventVaultCreated              = "VaultCreated"
	EventCollateralDeposited       = "CollateralDeposited"
	EventCollateralWithdrawn       = "CollateralWithdrawn"
	EventCollateralTransferred     = "CollateralTransferred"
	EventOrderPlaced               = "OrderPlaced"
	EventOrderOptions              = "OrderOptions"
	EventOrderCancelled            = "OrderCancelled"
	EventOrderExpired              = "OrderExpired"
	EventOrderTriggered            = "OrderTriggered"
	EventOrderExecuted             = "OrderExecuted"
	EventFill                      = "Fill"
	EventPositionOpened            = "PositionOpened"
	EventPositionModified          = "PositionModified"
	EventPositionEntryPriceChanged = "PositionEntryPriceChanged"
	EventPositionClosed            = "PositionClosed"
	EventPositionTpSlSet           = "PositionTpSlSet"
	EventTradeSettled              = "TradeSettled"
	EventFeeCollected              = "FeeCollected"
	EventMarkPriceUpdated          = "MarkPriceUpdated"
	EventFundingRateUpdated        = "FundingRateUpdated"
	EventFundingSettled            = "FundingSettled"
	EventPositionFundingApplied    = "PositionFundingApplied"
	EventOpenInterestChanged       = "OpenInterestChanged"
	EventLiquidation               = "Liquidation"
	EventBackstopLiquidation       = "BackstopLiquidation"
	EventInsuranceContribution     = "InsuranceContribution"
	EventInsuranceDraw             = "InsuranceDraw"
	EventVaultDeficit              = "VaultDeficit"
	EventADLExecuted               = "ADLExecuted"
	EventADLMatched                = "ADLMatched"
)

// EventRecord is one event the keeper collects: Name is an Event constant and
// Args are the event's arguments, indexed ones included, in the order of its
// ABI signature, as the values precompiles/perps/abi packs them.
type EventRecord struct {
	Name string
	Args []any
}
