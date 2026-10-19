package graphql

import (
	"context"
	"strings"
	"testing"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/ast"
)

func TestSchemaSDL(t *testing.T) {
	t.Run("definition", func(t *testing.T) {
		schema, err := graphql.ParseSchema(SDL, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{
			"Market",
			"Position",
			"Trade",
			"Order",
			"Liquidation",
			"PriceUpdate",
			"LatestPrice",
			"FundingRate",
			"UserVault",
			"VaultEvent",
			"CollateralToken",
			"PoolState",
			"FeeConfig",
			"ProtocolEvent",
			"IndexerStatus",
			"KeeperCycle",
			"AccountLedgerEntry",
			"Delegate",
			"TradingAccountEvent",
			"Fee",
			"TradeSettlement",
			"OiSnapshot",
			"MarkPriceRecord",
			"VaultBalanceRecord",
			"FundingPayment",
			"MarketSnapshotRecord",
			"ProtocolSnapshotRecord",
			"UserStats",
			"MarketStats",
			"GlobalStats",
			"OrderlyTicker",
			"OrderlyVolumeStats",
			"OrderlyPriceChange",
			"OrderlyOpenInterest",
			"OrderlyFundingRate",
			"EnrichedGlobalStats",
			"EnrichedMarketStats",
			"Query",
			"SpotMarket",
			"SpotOrder",
			"SpotOrderFill",
			"SpotBatchClearing",
			"SpotEpochSettlement",
			"SpotFastSettlement",
			"SpotCollateralMovement",
			"SpotMarketModeChange",
			"SpotPofqUpdate",
			"SpotPLV",
			"SpotKeeperCycle",
			"SpotBatchClearFailure",
		} {
			if _, ok := schema.AST().Types[name].(*ast.ObjectTypeDefinition); !ok {
				t.Errorf("missing application object %s", name)
			}
		}
		for _, name := range []string{"BigDecimal", "DateTime"} {
			if _, ok := schema.AST().Types[name].(*ast.ScalarTypeDefinition); !ok {
				t.Errorf("missing application scalar %s", name)
			}
		}
		if schema.AST().RootOperationTypes["mutation"] != nil {
			t.Error("account writes must not expose a mutation root")
		}
	})
	t.Run("parses", func(t *testing.T) {
		schema := newTestSchema(t)
		for operation, name := range map[string]string{"query": "Query", "subscription": "Subscription"} {
			root := schema.AST().RootOperationTypes[operation]
			if root == nil || root.TypeName() != name {
				t.Errorf("%s root = %v, want %s", operation, root, name)
			}
		}
		response := schema.Exec(context.Background(), "{ __typename }", "", nil)
		if len(response.Errors) != 0 || string(response.Data) != `{"__typename":"Query"}` {
			t.Fatalf("root execution: data=%s errors=%v", response.Data, response.Errors)
		}
	})
	t.Run("leaderboard_root", func(t *testing.T) {
		schema := newTestSchema(t)
		assertSchemaField(t, schema, "Query", "leaderboard", "[LeaderboardEntry!]!", map[string]string{
			"limit": "Int", "offset": "Int",
		})
		assertSchemaFields(t, schema, "LeaderboardEntry", map[string]string{
			"rank": "Int", "userAddress": "String", "totalRealizedPnl": "BigDecimal",
			"totalTrades": "Int", "winRate": "Float", "avgHoldTime": "Float",
			"openPositions": "Int", "totalVolume": "BigDecimal",
		})
		assertValidDocument(t, schema, `query GetLeaderboard($limit: Int, $offset: Int) {
            leaderboard(limit: $limit, offset: $offset) {
                rank userAddress totalRealizedPnl totalTrades winRate avgHoldTime openPositions totalVolume
            }
        }`, nil)
	})
	t.Run("margin_mode_field", func(t *testing.T) {
		schema := newTestSchema(t)
		assertSchemaField(t, schema, "UserVault", "marginMode", "Int", nil)
		assertValidDocument(t, schema, `query DashboardV2($userAddress: String!) {
            userVault(userAddress: $userAddress) { vaultAddress marginMode }
            positions(userAddress: $userAddress, status: "open") {
                positionId marketId isLong sizeUsd entryPrice collateralUsd leverage
                market { symbol latestPrice { price } }
            }
            orders(userAddress: $userAddress, status: "active") {
                orderId marketId orderType orderTypeName isLong triggerPrice sizeUsd
                positionId failureReason status
            }
            delegates(userAddress: $userAddress) { delegateAddress canTrade canWithdraw canModifyMargin expiry }
            userStats(userAddress: $userAddress) { totalPositions openPositions totalRealizedPnl totalTrades activeOrders }
        }`, map[string]any{"userAddress": "0x00000000000000000000000000000000000000a1"})
	})
	t.Run("subscription_fields", func(t *testing.T) {
		schema := newTestSchema(t)
		cases := []struct {
			name      string
			result    string
			args      map[string]string
			document  string
			variables map[string]any
		}{
			{"indexerStatusUpdated", "IndexerStatus", nil,
				`subscription { indexerStatusUpdated { lastIndexedBlock chainHead isSynced blocksScanned eventsProcessed } }`, nil},
			{"latestPricesUpdated", "[LatestPrice!]!", nil,
				`subscription { latestPricesUpdated { marketId price updatedAt blockNumber } }`, nil},
			{"priceUpdated", "PriceUpdate", map[string]string{"marketId": "Int"},
				`subscription PriceUpdated($marketId: Int!) { priceUpdated(marketId: $marketId) { marketId price updatedAt blockNumber } }`, map[string]any{"marketId": 0}},
			{"positionChanged", "[Position!]!", map[string]string{"userAddress": "String!"},
				`subscription PositionChanged($userAddress: String!) { positionChanged(userAddress: $userAddress) { positionId marketId isLong sizeUsd leverage entryPrice status realizedPnl } }`, map[string]any{"userAddress": "0x00000000000000000000000000000000000000a1"}},
			{"orderChanged", "Order", map[string]string{"userAddress": "String!"},
				`subscription OrderChanged($userAddress: String!) { orderChanged(userAddress: $userAddress) { orderId status marketId orderType remainingSize blockTimestamp } }`, map[string]any{"userAddress": "0x00000000000000000000000000000000000000a1"}},
			{"tradeCreated", "Trade", map[string]string{"marketId": "Int!"},
				`subscription TradeCreated($marketId: Int!) { tradeCreated(marketId: $marketId) { positionId userAddress sizeUsd executionPrice tradeType blockTimestamp } }`, map[string]any{"marketId": 0}},
			{"liquidationCreated", "Liquidation", map[string]string{"marketId": "Int!"},
				`subscription Liquidations($marketId: Int!) { liquidationCreated(marketId: $marketId) { positionId userAddress sizeUsd executionPrice blockTimestamp } }`, map[string]any{"marketId": 0}},
			{"tradeExecuted", "TradeExecuted", map[string]string{"marketId": "Int"},
				`subscription TradeExecuted($marketId: Int) { tradeExecuted(marketId: $marketId) { tradeId marketId isLong sizeUsd price blockTimestamp } }`, map[string]any{"marketId": 0}},
		}
		subscription, ok := schema.AST().RootOperationTypes["subscription"].(*ast.ObjectTypeDefinition)
		if !ok || len(subscription.Fields) != len(cases) {
			t.Fatalf("subscription root = %v, want exactly %d fields", subscription, len(cases))
		}
		for _, tc := range cases {
			assertSchemaField(t, schema, "Subscription", tc.name, tc.result, tc.args)
			assertValidDocument(t, schema, tc.document, tc.variables)
		}
		assertValidDocument(t, schema, `subscription PriceUpdated($marketId: Int) {
            priceUpdated(marketId: $marketId) { marketId price timestamp }
        }`, nil)
		assertValidDocument(t, schema, `subscription { priceUpdated { marketId price timestamp } }`, nil)
		assertValidDocument(t, schema, `subscription { tradeExecuted { tradeId marketId isLong sizeUsd price blockTimestamp } }`, nil)
	})
	t.Run("perfdb_roots", func(t *testing.T) {
		schema := newTestSchema(t)
		cases := []struct {
			name   string
			result string
			args   map[string]string
			fields string
		}{
			{"user", "User", map[string]string{"address": "String!"}, "address vaultAddress referralCode tier feeDiscountBps"},
			{"userBalances", "[UserBalance!]!", map[string]string{"address": "String!"}, "token balance totalDeposited totalWithdrawn depositCount withdrawCount"},
			{"userDailyPnl", "[DailyPnl!]!", map[string]string{"address": "String!", "limit": "Int"}, "date pnl volume trades"},
			{"userAlerts", "[UserAlert!]!", map[string]string{"address": "String!"}, "id alertType marketId threshold enabled triggeredAt"},
			{"userReferrals", "UserReferrals", map[string]string{"address": "String!"}, "referralCode tier referralCount totalEarned"},
			{"referralTiers", "[ReferralTier!]!", nil, "tier minReferrals minVolume rewardBps feeDiscountBps"},
			{"competitions", "[Competition!]!", nil, "id name description startAt endAt metric prizePool active"},
			{"competition", "Competition", map[string]string{"id": "String!"}, "id name description startAt endAt metric prizePool active rankings { rank address score }"},
		}
		for _, tc := range cases {
			assertSchemaField(t, schema, "Query", tc.name, tc.result, tc.args)
			arguments := ""
			if _, ok := tc.args["address"]; ok {
				arguments = `(address: "0x00000000000000000000000000000000000000a1")`
			}
			if _, ok := tc.args["id"]; ok {
				arguments = `(id: "contest")`
			}
			assertValidDocument(t, schema, "{ "+tc.name+arguments+" { "+tc.fields+" } }", nil)
		}
		assertValidDocument(t, schema, `query UserDailyPnl($address: String!, $limit: Int!) {
            userDailyPnl(address: $address, limit: $limit) { date pnl volume trades }
        }`, map[string]any{"address": "0x00000000000000000000000000000000000000a1", "limit": 30})
		assertSchemaField(t, schema, "Query", "userStats", "UserStats", map[string]string{
			"userAddress": "String", "address": "String",
		})
		assertSchemaFields(t, schema, "UserStats", map[string]string{
			"address": "String", "totalPnl": "BigDecimal", "totalVolume": "BigDecimal",
			"totalFeesPaid": "BigDecimal", "winCount": "Int", "lossCount": "Int", "liquidationCount": "Int",
			"userAddress": "String!", "totalPositions": "Int!", "openPositions": "Int!",
			"closedPositions": "Int!", "liquidatedPositions": "Int!", "totalTrades": "Int!",
			"totalRealizedPnl": "BigDecimal!", "totalOrders": "Int!", "activeOrders": "Int!",
		})
		assertValidDocument(t, schema, `query UserStats($address: String!) {
            userStats(address: $address) { address totalPnl totalVolume totalFeesPaid totalTrades winCount lossCount liquidationCount openPositions }
        }`, map[string]any{"address": "0x00000000000000000000000000000000000000a1"})
		for _, query := range []string{
			`{ userStats { address } }`,
			`{ userStats(userAddress: "0x00000000000000000000000000000000000000a1", address: "0x00000000000000000000000000000000000000a1") { address } }`,
		} {
			response := schema.Exec(context.Background(), query, "", nil)
			if len(response.Errors) != 1 || response.Errors[0].Message != "exactly one of userAddress or address is required" {
				t.Errorf("invalid userStats arguments: errors=%v", response.Errors)
			}
		}
	})
	t.Run("depth_limit", func(t *testing.T) {
		schema := newTestSchema(t)
		atLimit := `{ __type(name: "Position") { ` + strings.Repeat("ofType { ", 8) + "name" + strings.Repeat(" }", 10)
		response := schema.Exec(context.Background(), atLimit, "", nil)
		if len(response.Errors) != 0 {
			t.Fatalf("depth 10 refused: %v", response.Errors)
		}
		overLimit := `{ positions { positionId } __type(name: "Position") { ` + strings.Repeat("ofType { ", 9) + "name" + strings.Repeat(" }", 11)
		response = schema.Exec(context.Background(), overLimit, "", nil)
		if len(response.Errors) != 1 {
			t.Fatalf("depth 11 errors = %v, want one depth error", response.Errors)
		}
		err := response.Errors[0]
		if err.Rule != "MaxDepthExceeded" || err.Message != `Field "name" has depth 11 that exceeds max depth 10` {
			t.Fatalf("depth refusal: rule=%q message=%q", err.Rule, err.Message)
		}
		if len(response.Data) != 0 {
			t.Fatalf("over-deep query reached execution: data=%s", response.Data)
		}
	})
}

func newTestSchema(t *testing.T) *graphql.Schema {
	t.Helper()
	schema, err := NewSchema(nil)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func assertSchemaField(t *testing.T, schema *graphql.Schema, object, name, result string, args map[string]string) {
	t.Helper()
	typ, ok := schema.AST().Types[object].(*ast.ObjectTypeDefinition)
	if !ok {
		t.Fatalf("missing object %s", object)
	}
	field := typ.Fields.Get(name)
	if field == nil {
		t.Fatalf("missing field %s.%s", object, name)
	}
	if field.Type.String() != result {
		t.Errorf("%s.%s type = %s, want %s", object, name, field.Type.String(), result)
	}
	if len(field.Arguments) != len(args) {
		t.Errorf("%s.%s has %d arguments, want %d", object, name, len(field.Arguments), len(args))
	}
	for arg, want := range args {
		value := field.Arguments.Get(arg)
		if value == nil {
			t.Errorf("missing argument %s.%s(%s)", object, name, arg)
		} else if value.Type.String() != want {
			t.Errorf("%s.%s(%s) type = %s, want %s", object, name, arg, value.Type.String(), want)
		}
	}
}

func assertSchemaFields(t *testing.T, schema *graphql.Schema, object string, fields map[string]string) {
	t.Helper()
	for name, result := range fields {
		assertSchemaField(t, schema, object, name, result, nil)
	}
}

func assertValidDocument(t *testing.T, schema *graphql.Schema, document string, variables map[string]any) {
	t.Helper()
	if errors := schema.ValidateWithVariables(document, variables); len(errors) != 0 {
		t.Errorf("document rejected: %v", errors)
	}
}
