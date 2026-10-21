package coverage

import (
	_ "embed"
	"encoding/json"
)

//go:embed authorities.json
var authorities []byte

type Family struct {
	Family, Primary, Rule string
	Corroboration         []string
	Source                []string
	CoverageLimit         string `json:"coverage_limit"`
}

func Families() ([]Family, error) {
	var d struct{ Families []Family }
	e := json.Unmarshal(authorities, &d)
	return d.Families, e
}

type Rule struct {
	Kind, Effect             string
	RequiresCallerPermission bool
}

// Authority defaults to supplemental. A declared ABI alone is never proof of
// concrete emission. Callers still validate registered origin and permissions.
func Authority(role, event string) Rule {
	if role == "EventEmitter" {
		switch event {
		case "RoleGranted", "RoleRevoked", "RoleAdminChanged", "Upgraded", "TypedEmitterPermission":
			return Rule{"primary", "control_transition", false}
		case "Swap":
			return Rule{"conditional_primary", "core_fill", true}
		case "PoolStateUpdated":
			return Rule{"conditional_primary", "reserve_state", true}
		case "ConfigUpdated":
			return Rule{"conditional_primary", "configuration", true}
		case "EventLog", "EventLog1", "EventLog2":
			return Rule{"supplemental", "none", true}
		default:
			return Rule{"corroboration", "none", true}
		}
	}
	if role == "SidioraFactory" && event == "MarketCreated" {
		return Rule{"primary", "market_attachment", false}
	}
	if role == "Router" {
		switch event {
		case "Buy", "Sell", "MultihopSwap":
			return Rule{"route_primary", "route_execution", false}
		case "MarketCreated":
			return Rule{"corroboration", "none", false}
		}
	}
	if role == "SidioraERC20" || role == "SidioraNFT" {
		switch event {
		case "Transfer":
			return Rule{"primary", "asset_movement", false}
		case "Approval", "ApprovalForAll":
			return Rule{"primary", "asset_permission", false}
		case "PoolNFTMinted", "FeeStrategyChanged":
			return Rule{"primary", "nft_strategy", false}
		}
	}
	if role == "FeeAccumulator" {
		switch event {
		case "FeeRecorded", "FeesClaimed", "FeesBurned", "AirdropTriggered", "AirdropClaimed", "LpRewardsSent", "OpticalSurplusRecorded", "OpticalSurplusClaimed":
			return Rule{"primary", event, false}
		case "ProtocolFeeSwept":
			return Rule{"declared_not_emitted", "none", false}
		}
	}
	if role == "PECOR" {
		switch event {
		case "SimpleSwap", "MarketOrderExecuted", "NativeSwap", "FeesCollected":
			return Rule{"primary", event, false}
		case "TrackerCallFailed":
			return Rule{"diagnostic", "enrichment_missing", false}
		}
	}
	if role == "MetaAGRouter" && (event == "BestRouteSwap" || event == "MultiHopSwap") {
		return Rule{"primary", "route_execution", false}
	}
	if role == "TransactionTracker" {
		return Rule{"corroboration", "none", false}
	}
	if role == "PECOROrders" {
		switch event {
		case "LimitOrderCreated", "StopLimitOrderCreated":
			return Rule{"primary", "order_created", false}
		case "StopLimitActivated":
			return Rule{"primary", "order_triggered", false}
		case "LimitOrderExecuted", "StopLimitExecuted":
			return Rule{"primary", "order_filled", false}
		case "OrderExecutionFailed":
			return Rule{"diagnostic", "order_unchanged", false}
		case "OrderExpiredAndRefunded":
			return Rule{"primary", "expired_refunded", false}
		case "LimitOrderCancelled", "StopLimitCancelled":
			return Rule{"primary", "order_cancelled", false}
		case "OrderMinimumOutputUpdated":
			return Rule{"primary", "minimum_output", false}
		}
	}
	if role == "PECORVault" {
		switch event {
		case "Deposit", "Withdrawal", "NativeDeposit", "NativeWithdrawal", "EmergencyWithdraw":
			return Rule{"primary", "funding_movement", false}
		case "ReservesUpdated", "ReservesSync":
			return Rule{"primary", "reserve_state", false}
		case "TokenRegistered", "StablecoinStatusUpdated":
			return Rule{"primary", "asset_configuration", false}
		}
	}
	if role == "OpticalRegistry" {
		switch event {
		case "OpticalDeregistered", "OpticalRegistered", "OpticalIdentityReviewed", "OpticalMetadataUpdated":
			return Rule{"primary", "optical_configuration", false}
		}
	}
	switch event {
	case "RoleGranted", "RoleRevoked", "Upgraded", "PauseToggled", "OwnershipTransferStarted", "OwnershipTransferred", "GuardianTransferStarted", "GuardianTransferred":
		return Rule{"primary", "control_transition", false}
	}
	return Rule{"supplemental", "none", false}
}
func CoreSwapCoverage(mirrorAuthorized bool) string {
	if mirrorAuthorized {
		return "authorized_mirror_required"
	}
	return "coverage_gap_direct_swaps"
}
