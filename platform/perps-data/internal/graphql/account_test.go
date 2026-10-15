package graphql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graph-gophers/graphql-go"
)

func accountTestExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func accountTestSchema(t *testing.T, db *sql.DB) *gql.Schema {
	t.Helper()
	start := strings.Index(SDL, "type UserStats {")
	if start < 0 {
		t.Fatal("UserStats missing from SDL")
	}
	end := strings.Index(SDL[start:], "\n}")
	if end < 0 {
		t.Fatal("UserStats definition is incomplete")
	}
	root := "  userStats(userAddress: String, address: String): UserStats"
	if !strings.Contains(SDL, root) {
		t.Fatal("userStats root missing from SDL")
	}
	schema, err := gql.ParseSchema("scalar BigDecimal\n"+SDL[start:start+end+2]+"\n"+
		strings.Replace(PerfdbSDL, "extend type Query {", "type Query {\n"+root, 1), &Resolver{DB: db},
		gql.UseFieldResolvers(), gql.MaxDepth(10), gql.MaxParallelism(20))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func accountTestQuery(t *testing.T, schema *gql.Schema, query string, variables map[string]any, want string) {
	t.Helper()
	response := schema.Exec(context.Background(), query, "", variables)
	if len(response.Errors) != 0 {
		t.Fatalf("query errors: %v", response.Errors)
	}
	accountTestJSON(t, response.Data, want)
}

func accountTestJSON(t *testing.T, data []byte, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(data, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("got %s, want %s", data, want)
	}
}

func TestAccountResolvers(t *testing.T) {
	databaseURL := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	accountTestExec(t, db, `TRUNCATE perps.vaults, perps.collateral_events, perps.account_margin,
        perps.user_stats, perps.user_pnl_daily, perps.user_alerts, perps.referrals, perps.referral_tiers,
        perps.competition_entries, perps.competitions, perps.positions_current, perps.orders,
        perps.position_events, perps.trade_settlements, perps.fills RESTART IDENTITY`)
	now := time.Now().UTC().Truncate(time.Second)
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	a := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	b := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	c := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	missing := "0x00000000000000000000000000000000000000d1"
	vault := common.HexToAddress("0x0000000000000000000000000000000000001015")
	address := strings.ToLower(a.Hex())
	vars := map[string]any{"address": strings.ToUpper(address)}
	commonColumns := `block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account`
	sequence := 0
	fact := func(t *testing.T, table, kind string, account common.Address, at time.Time, columns string, values ...any) {
		t.Helper()
		sequence++
		hash := common.BigToHash(common.Big1)
		args := []any{sequence, hash.Bytes(), sequence, at, vault.Bytes(), kind, account.Bytes()}
		placeholders := `$1, $2, 0, $3, 0, $2, 'perps_endblock', $4, $5, $6, $7`
		for _, value := range values {
			args = append(args, value)
			placeholders += fmt.Sprintf(", $%d", len(args))
		}
		accountTestExec(t, db, "INSERT INTO perps."+table+" ("+commonColumns+", "+columns+") VALUES ("+placeholders+")", args...)
	}
	fact(t, "vaults", "created", a, now, `"user", vault`, a.Bytes(), vault.Bytes())
	for _, row := range []struct{ kind, amount, balance string }{
		{"deposited", "5000000", "5000000"}, {"deposited", "5000000", "10000000"},
		{"withdrawn", "1000000", "9000000"}, {"transferred_in", "3000000", "12000000"},
	} {
		fact(t, "collateral_events", row.kind, a, now, "amount, new_balance", row.amount, row.balance)
	}
	fact(t, "collateral_events", "deposited", b, now, "amount, new_balance", "999999", "999999")
	accountTestExec(t, db, `INSERT INTO perps.account_margin VALUES ($1,$2,12000000,1000000,0,1)`, a.Bytes(), store.USDL.Bytes())
	accountTestExec(t, db, `INSERT INTO perps.user_stats VALUES ($1,7,9007199254740993,12,-5,0,-17,2,3,1,1)`, a.Bytes())
	accountTestExec(t, db, `INSERT INTO perps.user_pnl_daily
        SELECT $1, ($2::timestamptz AT TIME ZONE 'UTC')::date - n, n, 2, -3, n-5, 9007199254740993+n, n
        FROM generate_series(0,501) n`, a.Bytes(), now)
	accountTestExec(t, db, `INSERT INTO perps.referrals(address,referral_code,total_earned) VALUES ($1,'CODE',73)`, a.Bytes())
	accountTestExec(t, db, `INSERT INTO perps.referrals(address,referred_by,applied_at) VALUES ($1,$3,$4),($2,$3,$4)`, b.Bytes(), c.Bytes(), a.Bytes(), now)
	accountTestExec(t, db, `INSERT INTO perps.referral_tiers VALUES
        ('bronze',0,0,10,0),('silver',2,9007199254740993,20,5),('gold',2,9007199254740994,30,10),('platinum',3,0,40,15)`)
	accountTestExec(t, db, `INSERT INTO perps.user_alerts(address,alert_type,market_id,threshold,enabled,triggered_at)
        VALUES ($1,'price_above',1,123.125,true,$3),($1,'funding_threshold',NULL,NULL,false,NULL),($2,'price_below',2,3,true,NULL)`, a.Bytes(), b.Bytes(), now)
	for i, status := range []string{"open", "closed", "liquidated"} {
		accountTestExec(t, db, `INSERT INTO perps.positions_current(position_id,account,market_id,is_long,size_usd,
            collateral_usd,collateral_amount,collateral_token,entry_price,leverage,realized_pnl,status,opened_block)
            VALUES ($1,$2,1,true,100,10,10,$3,100,10,0,$4,1)`, i+1, a.Bytes(), store.USDL.Bytes(), status)
	}
	for i, status := range []string{"active", "filled", "cancelled", "triggered"} {
		accountTestExec(t, db, `INSERT INTO perps.orders(order_id,account,market_id,order_type,is_long,trigger_price,size_usd,remaining_size_usd,status,placed_block)
            VALUES ($1,$2,1,0,true,100,100,100,$3,1)`, i+1, a.Bytes(), status)
	}
	accountTestExec(t, db, `INSERT INTO perps.competitions(name,description,metric,start_at,end_at,prize_pool,status)
        VALUES ('Profit',NULL,'pnl',$1,$2,9007199254740993,'active'),('Volume','Trading volume','volume',$1,$2,25,'active'),
            ('Past',NULL,'pnl',$3,$1,1,'ended'),('Future',NULL,'pnl',$2,$4,2,'active')`, start, end, start.Add(-time.Hour), end.Add(time.Hour))
	accountTestExec(t, db, `INSERT INTO perps.competition_entries(competition_id,address,joined_at)
        SELECT id, addr, $4 FROM perps.competitions CROSS JOIN (VALUES ($1::bytea),($2::bytea),($3::bytea)) a(addr)
        WHERE id IN (1,2)`, a.Bytes(), b.Bytes(), c.Bytes(), start)
	for _, row := range []struct {
		owner common.Address
		at    time.Time
		pnl   string
	}{
		{a, start, "1000"}, {a, now, "8"}, {a, end, "2"}, {a, end.Add(time.Second), "1000"},
		{b, now, "10"}, {c, now, "-3"}, {common.HexToAddress(missing), now, "9999"},
	} {
		fact(t, "position_events", "closed", row.owner, row.at, "position_id, realized_pnl", sequence+1, row.pnl)
	}
	settlement := func(t *testing.T, owner common.Address, at time.Time, size string) {
		t.Helper()
		fact(t, "trade_settlements", "settled", owner, at,
			`position_id, "user", market_id, trade_type, size_usd, execution_price, gross_pnl, total_fees_usd, funding_paid_usd, net_payout_tokens`,
			sequence+1, owner.Bytes(), "1", 0, size, "100", "999", "0", "0", "0")
	}
	settlement(t, a, start, "10000")
	settlement(t, a, now, "2")
	settlement(t, a, end, "3")
	settlement(t, a, end.Add(time.Second), "10000")
	settlement(t, b, now, "9007199254740993")
	settlement(t, c, now, "1")
	schema := accountTestSchema(t, db)

	t.Run("user", func(t *testing.T) {
		accountTestQuery(t, schema, `query User($address:String!){user(address:$address){address vaultAddress referralCode tier feeDiscountBps}}`, vars,
			`{"user":{"address":"`+address+`","vaultAddress":"`+strings.ToLower(vault.Hex())+`","referralCode":"CODE","tier":"silver","feeDiscountBps":5}}`)
	})
	t.Run("userBalances", func(t *testing.T) {
		accountTestQuery(t, schema, `query UserBalances($address:String!){userBalances(address:$address){token balance totalDeposited totalWithdrawn depositCount withdrawCount}}`, vars,
			`{"userBalances":[{"token":"`+strings.ToLower(store.USDL.Hex())+`","balance":"12000000","totalDeposited":"10000000","totalWithdrawn":"1000000","depositCount":2,"withdrawCount":1}]}`)
	})
	t.Run("userDailyPnl", func(t *testing.T) {
		accountTestQuery(t, schema, `query UserDailyPnl($address:String!){userDailyPnl(address:$address,limit:1){date pnl volume trades}}`, vars,
			`{"userDailyPnl":[{"date":"`+now.Format(time.DateOnly)+`","pnl":"-5","volume":"9007199254740993","trades":0}]}`)
		for _, tc := range []struct {
			limit string
			count int
		}{{"", 50}, {",limit:0", 1}, {",limit:-4", 1}, {",limit:501", 500}} {
			response := schema.Exec(context.Background(), `query($address:String!){userDailyPnl(address:$address`+tc.limit+`){date}}`, "", vars)
			if len(response.Errors) != 0 {
				t.Fatal(response.Errors)
			}
			var data struct{ UserDailyPnl []struct{ Date string } }
			if err := json.Unmarshal(response.Data, &data); err != nil {
				t.Fatal(err)
			}
			if len(data.UserDailyPnl) != tc.count {
				t.Fatalf("limit %s: got %d rows, want %d", tc.limit, len(data.UserDailyPnl), tc.count)
			}
			if data.UserDailyPnl[tc.count-1].Date != now.AddDate(0, 0, 1-tc.count).Format(time.DateOnly) {
				t.Fatal("daily rows not in descending order")
			}
		}
	})
	statsFields := `address totalPnl totalVolume totalFeesPaid winCount lossCount liquidationCount userAddress totalPositions openPositions closedPositions liquidatedPositions totalTrades totalRealizedPnl totalOrders activeOrders`
	statsJSON := `{"userStats":{"address":"` + address + `","totalPnl":"-17","totalVolume":"9007199254740993","totalFeesPaid":"12","winCount":2,"lossCount":3,"liquidationCount":1,"userAddress":"` + address + `","totalPositions":3,"openPositions":1,"closedPositions":1,"liquidatedPositions":1,"totalTrades":7,"totalRealizedPnl":"0","totalOrders":4,"activeOrders":1}}`
	t.Run("userStats", func(t *testing.T) {
		accountTestQuery(t, schema, `query($address:String!){userStats(userAddress:$address){`+statsFields+`}}`, vars, statsJSON)
	})
	t.Run("user_stats_by_address", func(t *testing.T) {
		accountTestQuery(t, schema, `query UserStats($address:String!){userStats(address:$address){`+statsFields+`}}`, vars, statsJSON)
	})
	for _, tc := range []struct{ name, args string }{
		{"user_stats_both_args_refused", `(userAddress:"` + address + `",address:"` + address + `")`},
		{"user_stats_no_args_refused", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := schema.Exec(context.Background(), `{userStats`+tc.args+`{totalPnl}}`, "", nil)
			if len(response.Errors) != 1 || response.Errors[0].Message != "exactly one of userAddress and address is required" {
				t.Fatalf("errors = %v", response.Errors)
			}
			accountTestJSON(t, response.Data, `{"userStats":null}`)
		})
	}
	t.Run("userAlerts", func(t *testing.T) {
		accountTestQuery(t, schema, `query UserAlerts($address:String!){userAlerts(address:$address){id alertType marketId threshold enabled triggeredAt}}`, vars,
			`{"userAlerts":[{"id":1,"alertType":"price_above","marketId":1,"threshold":"123.125","enabled":true,"triggeredAt":"`+now.Format(time.RFC3339)+`"},{"id":2,"alertType":"funding_threshold","marketId":null,"threshold":null,"enabled":false,"triggeredAt":null}]}`)
	})
	tiers := `[{"tier":"bronze","minReferrals":0,"minVolume":"0","rewardBps":10,"feeDiscountBps":0},{"tier":"silver","minReferrals":2,"minVolume":"9007199254740993","rewardBps":20,"feeDiscountBps":5},{"tier":"gold","minReferrals":2,"minVolume":"9007199254740994","rewardBps":30,"feeDiscountBps":10},{"tier":"platinum","minReferrals":3,"minVolume":"0","rewardBps":40,"feeDiscountBps":15}]`
	tierFields := `tier minReferrals minVolume rewardBps feeDiscountBps`
	t.Run("userReferrals", func(t *testing.T) {
		accountTestQuery(t, schema, `query UserReferrals($address:String!){userReferrals(address:$address){referralCode tier referralCount totalEarned} referralTiers{`+tierFields+`}}`, vars,
			`{"userReferrals":{"referralCode":"CODE","tier":"silver","referralCount":2,"totalEarned":"73"},"referralTiers":`+tiers+`}`)
		accountTestQuery(t, schema, `{userReferrals(address:"`+strings.ToLower(b.Hex())+`"){referralCode tier referralCount totalEarned}}`, nil,
			`{"userReferrals":{"referralCode":null,"tier":"bronze","referralCount":0,"totalEarned":"0"}}`)
	})
	t.Run("referralTiers", func(t *testing.T) {
		accountTestQuery(t, schema, `query ReferralTiers{referralTiers{`+tierFields+`}}`, nil, `{"referralTiers":`+tiers+`}`)
	})
	competitionFields := `id name description startAt endAt metric prizePool active`
	profit := `{"id":"1","name":"Profit","description":null,"startAt":"` + start.Format(time.RFC3339) + `","endAt":"` + end.Format(time.RFC3339) + `","metric":"pnl","prizePool":"9007199254740993","active":true}`
	volume := `{"id":"2","name":"Volume","description":"Trading volume","startAt":"` + start.Format(time.RFC3339) + `","endAt":"` + end.Format(time.RFC3339) + `","metric":"volume","prizePool":"25","active":true}`
	t.Run("competitions", func(t *testing.T) {
		future := `{"id":"4","name":"Future","description":null,"startAt":"` + end.Format(time.RFC3339) + `","endAt":"` + end.Add(time.Hour).Format(time.RFC3339) + `","metric":"pnl","prizePool":"2","active":false}`
		past := `{"id":"3","name":"Past","description":null,"startAt":"` + start.Add(-time.Hour).Format(time.RFC3339) + `","endAt":"` + start.Format(time.RFC3339) + `","metric":"pnl","prizePool":"1","active":false}`
		accountTestQuery(t, schema, `query Competitions{competitions{`+competitionFields+`}}`, nil, `{"competitions":[`+future+`,`+volume+`,`+profit+`,`+past+`]}`)
	})
	t.Run("competition", func(t *testing.T) {
		ranks := `[{"rank":1,"address":"` + address + `","score":"10"},{"rank":2,"address":"` + strings.ToLower(b.Hex()) + `","score":"10"},{"rank":3,"address":"` + strings.ToLower(c.Hex()) + `","score":"-3"}]`
		accountTestQuery(t, schema, `query Competition($id:String!){competition(id:$id){`+competitionFields+` rankings{rank address score}}}`, map[string]any{"id": "1"},
			`{"competition":`+strings.TrimSuffix(profit, "}")+`,"rankings":`+ranks+`}}`)
		ranks = `[{"rank":1,"address":"` + strings.ToLower(b.Hex()) + `","score":"9007199254740993"},{"rank":2,"address":"` + address + `","score":"5"},{"rank":3,"address":"` + strings.ToLower(c.Hex()) + `","score":"1"}]`
		accountTestQuery(t, schema, `{competition(id:"2"){rankings{rank address score}}}`, nil, `{"competition":{"rankings":`+ranks+`}}`)
		accountTestQuery(t, schema, `{competition(id:"3"){rankings{rank}}}`, nil, `{"competition":{"rankings":[]}}`)
	})
	t.Run("unknown_account", func(t *testing.T) {
		accountTestQuery(t, schema, `query($address:String!){user(address:$address){address} userBalances(address:$address){token} userDailyPnl(address:$address){date} userStats(address:$address){address} userAlerts(address:$address){id} userReferrals(address:$address){tier} competition(id:"9999"){id}}`, map[string]any{"address": missing},
			`{"user":null,"userBalances":[],"userDailyPnl":[],"userStats":null,"userAlerts":[],"userReferrals":null,"competition":null}`)
	})
	t.Run("invalid_arguments", func(t *testing.T) {
		for _, query := range []string{`{user(address:"bad"){address}}`, `{userBalances(address:"bad"){token}}`, `{userDailyPnl(address:"bad"){date}}`, `{userStats(address:"bad"){address}}`, `{userAlerts(address:"bad"){id}}`, `{userReferrals(address:"bad"){tier}}`} {
			response := schema.Exec(context.Background(), query, "", nil)
			if len(response.Errors) != 1 || response.Errors[0].Message != "invalid address" {
				t.Fatalf("errors = %v", response.Errors)
			}
		}
		for _, id := range []string{"0", "-1", "bad", "9223372036854775808"} {
			response := schema.Exec(context.Background(), `{competition(id:"`+id+`"){id}}`, "", nil)
			if len(response.Errors) != 1 || response.Errors[0].Message != "invalid competition id" {
				t.Fatalf("errors = %v", response.Errors)
			}
		}
	})
	t.Run("int_overflow_refused", func(t *testing.T) {
		accountTestExec(t, db, `UPDATE perps.user_stats SET trades_count = 2147483648 WHERE account=$1`, a.Bytes())
		defer accountTestExec(t, db, `UPDATE perps.user_stats SET trades_count = 7 WHERE account=$1`, a.Bytes())
		response := schema.Exec(context.Background(), `query($address:String!){userStats(address:$address){totalTrades}}`, "", vars)
		if len(response.Errors) != 1 || !strings.Contains(response.Errors[0].Message, "value out of range") {
			t.Fatalf("overflow errors = %v", response.Errors)
		}
		accountTestJSON(t, response.Data, `{"userStats":null}`)
	})
	t.Run("schema_integration", func(t *testing.T) {
		full, err := NewSchema(db)
		if err != nil {
			t.Fatal(err)
		}
		accountTestQuery(t, full, `query($address:String!){userStats(address:$address){`+statsFields+`}}`, vars, statsJSON)
	})
	t.Run("trade_executed", func(t *testing.T) {
		full, err := NewSchema(db)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var streams []<-chan any
		for _, market := range []any{int32(1), nil} {
			stream, err := full.Subscribe(ctx, `subscription TradeExecuted($marketId:Int){tradeExecuted(marketId:$marketId){tradeId marketId isLong sizeUsd price blockTimestamp}}`, "", map[string]any{"marketId": market})
			if err != nil {
				t.Fatal(err)
			}
			streams = append(streams, stream)
			select {
			case message, ok := <-stream:
				if !ok {
					t.Fatal("tradeExecuted subscription closed before a Fill")
				}
				t.Fatalf("tradeExecuted emitted before a Fill: %v", message)
			case <-time.After(10 * time.Millisecond):
			}
		}
		insertFill := func(market string) string {
			fact(t, "fills", "fill", a, now,
				"market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price, size_usd, maker_fee_usd, taker_fee_usd",
				market, "1", "2", b.Bytes(), a.Bytes(), true, "12345678901234567890", "9007199254740993", "-2", "3")
			var id string
			if err := db.QueryRow(`SELECT row_id::text FROM perps.fills WHERE log_index=$1`, sequence).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		receive := func(stream <-chan any, market int, id string) {
			select {
			case message, ok := <-stream:
				if !ok {
					t.Fatal("tradeExecuted closed without delivering Fill")
				}
				response, ok := message.(*gql.Response)
				if !ok {
					t.Fatalf("subscription response type %T", message)
				}
				if len(response.Errors) != 0 {
					t.Fatal(response.Errors)
				}
				accountTestJSON(t, response.Data, fmt.Sprintf(`{"tradeExecuted":{"tradeId":%q,"marketId":%d,"isLong":true,"sizeUsd":"9007199254740993","price":"12345678901234567890","blockTimestamp":%d}}`, id, market, now.Unix()))
			case <-ctx.Done():
				t.Fatal("Fill was not delivered through the subscription hub")
			}
		}
		id := insertFill("1")
		settlement(t, a, now, "9007199254740993")
		settlement(t, b, now, "9007199254740993")
		for _, stream := range streams {
			receive(stream, 1, id)
		}
		id = insertFill("2")
		receive(streams[1], 2, id)
		for _, stream := range streams {
			select {
			case message := <-stream:
				t.Fatalf("duplicate or wrong-market tradeExecuted message: %v", message)
			case <-time.After(25 * time.Millisecond):
			}
		}
	})
}
