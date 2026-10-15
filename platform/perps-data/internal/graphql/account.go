package graphql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

type accountAmount string

func (accountAmount) ImplementsGraphQLType(name string) bool { return name == "BigDecimal" }

func (v *accountAmount) UnmarshalGraphQL(input any) error {
	raw, ok := input.(string)
	if !ok {
		return errors.New("BigDecimal requires a decimal string")
	}
	var err error
	if len(raw) > 0 && raw[0] == '-' {
		err = bigint.ValidateRawInt256(raw)
	} else {
		err = bigint.ValidateRaw(raw)
	}
	if err != nil {
		return err
	}
	*v = accountAmount(raw)
	return nil
}

type accountUser struct {
	Address        *string
	VaultAddress   *string
	ReferralCode   *string
	Tier           *string
	FeeDiscountBps *int32
}

type accountBalance struct {
	Token          *string
	Balance        *accountAmount
	TotalDeposited *accountAmount
	TotalWithdrawn *accountAmount
	DepositCount   *int32
	WithdrawCount  *int32
}

type accountDailyPnl struct {
	Date   *string
	Pnl    *accountAmount
	Volume *accountAmount
	Trades *int32
}

type accountStats struct {
	Address             *string
	TotalPnl            *accountAmount
	TotalVolume         *accountAmount
	TotalFeesPaid       *accountAmount
	WinCount            *int32
	LossCount           *int32
	LiquidationCount    *int32
	UserAddress         string
	TotalPositions      int32
	OpenPositions       int32
	ClosedPositions     int32
	LiquidatedPositions int32
	TotalTrades         int32
	TotalRealizedPnl    accountAmount
	TotalOrders         int32
	ActiveOrders        int32
}

type accountAlert struct {
	ID          *int32
	AlertType   *string
	MarketID    *int32
	Threshold   *string
	Enabled     *bool
	TriggeredAt *string
}

type accountReferrals struct {
	ReferralCode  *string
	Tier          *string
	ReferralCount *int32
	TotalEarned   *accountAmount
}

type accountReferralTier struct {
	Tier           *string
	MinReferrals   *int32
	MinVolume      *accountAmount
	RewardBps      *int32
	FeeDiscountBps *int32
}

type accountCompetition struct {
	ID             *string
	Name           *string
	Description    *string
	StartAt        *string
	EndAt          *string
	Metric         *string
	PrizePool      *accountAmount
	Active         *bool
	db             *sql.DB
	competitionKey int64
}

type accountCompetitionEntry struct {
	Rank    *int32
	Address *string
	Score   *accountAmount
}

func accountAddress(raw string) (common.Address, error) {
	if len(raw) != 2+2*common.AddressLength || !common.IsHexAddress(raw) {
		return common.Address{}, errors.New("invalid address")
	}
	return common.HexToAddress(raw), nil
}

const accountTierJoin = `LEFT JOIN LATERAL (
    SELECT tier, fee_discount_bps FROM perps.referral_tiers
    WHERE min_referrals <= (SELECT count(*) FROM perps.referrals WHERE referred_by = $1)
      AND min_volume <= COALESCE((SELECT volume_usd FROM perps.user_stats WHERE account = $1), 0)
    ORDER BY min_referrals DESC, min_volume DESC, tier LIMIT 1
) tier ON true`

func (r *Resolver) User(ctx context.Context, args struct{ Address string }) (*accountUser, error) {
	address, err := accountAddress(args.Address)
	if err != nil {
		return nil, err
	}
	var out accountUser
	err = r.DB.QueryRowContext(ctx, `SELECT '0x' || encode(v."user", 'hex'), '0x' || encode(v.vault, 'hex'),
        ref.referral_code, tier.tier, COALESCE(tier.fee_discount_bps, 0)
        FROM perps.vaults v LEFT JOIN perps.referrals ref ON ref.address = v."user"
        `+accountTierJoin+` WHERE v."user" = $1`, address.Bytes()).Scan(
		&out.Address, &out.VaultAddress, &out.ReferralCode, &out.Tier, &out.FeeDiscountBps)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Resolver) UserBalances(ctx context.Context, args struct{ Address string }) ([]*accountBalance, error) {
	address, err := accountAddress(args.Address)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT '0x' || encode(m.token, 'hex'), trunc(m.balance)::text,
        trunc(COALESCE(e.deposited, 0))::text, trunc(COALESCE(e.withdrawn, 0))::text,
        COALESCE(e.deposits, 0), COALESCE(e.withdrawals, 0)
        FROM perps.account_margin m LEFT JOIN LATERAL (
            SELECT sum(amount) FILTER (WHERE kind = 'deposited') AS deposited,
                sum(amount) FILTER (WHERE kind = 'withdrawn') AS withdrawn,
                count(*) FILTER (WHERE kind = 'deposited') AS deposits,
                count(*) FILTER (WHERE kind = 'withdrawn') AS withdrawals
            FROM perps.collateral_events WHERE account = m.account
        ) e ON m.token = $2 WHERE m.account = $1 ORDER BY m.token`, address.Bytes(), store.USDL.Bytes())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountBalance{}
	for rows.Next() {
		row := new(accountBalance)
		if err := rows.Scan(&row.Token, &row.Balance, &row.TotalDeposited, &row.TotalWithdrawn, &row.DepositCount, &row.WithdrawCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Resolver) UserDailyPnl(ctx context.Context, args struct {
	Address string
	Limit   *int32
}) ([]*accountDailyPnl, error) {
	address, err := accountAddress(args.Address)
	if err != nil {
		return nil, err
	}
	limit := int32(50)
	if args.Limit != nil {
		limit = max(1, min(500, *args.Limit))
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), trunc(realized_pnl)::text,
        trunc(volume)::text, trades FROM perps.user_pnl_daily WHERE account = $1 ORDER BY day DESC LIMIT $2`, address.Bytes(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountDailyPnl{}
	for rows.Next() {
		row := new(accountDailyPnl)
		if err := rows.Scan(&row.Date, &row.Pnl, &row.Volume, &row.Trades); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Resolver) UserStats(ctx context.Context, args struct {
	UserAddress *string
	Address     *string
}) (*accountStats, error) {
	if (args.UserAddress == nil) == (args.Address == nil) {
		return nil, errors.New("exactly one of userAddress and address is required")
	}
	raw := args.Address
	if raw == nil {
		raw = args.UserAddress
	}
	address, err := accountAddress(*raw)
	if err != nil {
		return nil, err
	}
	var out accountStats
	err = r.DB.QueryRowContext(ctx, `SELECT '0x' || encode(s.account, 'hex'), '0x' || encode(s.account, 'hex'),
        trunc(s.total_pnl)::text, trunc(s.volume_usd)::text, trunc(s.fees_usd)::text,
        s.win_count, s.loss_count, s.liquidations, s.trades_count, trunc(s.realized_pnl)::text,
        p.total, p.open, p.closed, p.liquidated, o.total, o.active
        FROM perps.user_stats s CROSS JOIN LATERAL (
            SELECT count(*) AS total, count(*) FILTER (WHERE status = 'open') AS open,
                count(*) FILTER (WHERE status = 'closed') AS closed,
                count(*) FILTER (WHERE status = 'liquidated') AS liquidated
            FROM perps.positions_current WHERE account = s.account
        ) p CROSS JOIN LATERAL (
            SELECT count(*) AS total, count(*) FILTER (WHERE status = 'active') AS active
            FROM perps.orders WHERE account = s.account
        ) o WHERE s.account = $1`, address.Bytes()).Scan(
		&out.Address, &out.UserAddress, &out.TotalPnl, &out.TotalVolume, &out.TotalFeesPaid,
		&out.WinCount, &out.LossCount, &out.LiquidationCount, &out.TotalTrades, &out.TotalRealizedPnl,
		&out.TotalPositions, &out.OpenPositions, &out.ClosedPositions, &out.LiquidatedPositions,
		&out.TotalOrders, &out.ActiveOrders)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Resolver) UserAlerts(ctx context.Context, args struct{ Address string }) ([]*accountAlert, error) {
	address, err := accountAddress(args.Address)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id, alert_type, market_id, threshold::text, enabled, triggered_at
        FROM perps.user_alerts WHERE address = $1 ORDER BY id`, address.Bytes())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountAlert{}
	for rows.Next() {
		row := new(accountAlert)
		var triggered sql.NullTime
		if err := rows.Scan(&row.ID, &row.AlertType, &row.MarketID, &row.Threshold, &row.Enabled, &triggered); err != nil {
			return nil, err
		}
		if triggered.Valid {
			value := triggered.Time.UTC().Format(time.RFC3339Nano)
			row.TriggeredAt = &value
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Resolver) UserReferrals(ctx context.Context, args struct{ Address string }) (*accountReferrals, error) {
	address, err := accountAddress(args.Address)
	if err != nil {
		return nil, err
	}
	var out accountReferrals
	err = r.DB.QueryRowContext(ctx, `SELECT ref.referral_code, tier.tier,
        (SELECT count(*) FROM perps.referrals WHERE referred_by = $1), trunc(ref.total_earned)::text
        FROM perps.referrals ref `+accountTierJoin+` WHERE ref.address = $1`, address.Bytes()).Scan(
		&out.ReferralCode, &out.Tier, &out.ReferralCount, &out.TotalEarned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Resolver) ReferralTiers(ctx context.Context) ([]*accountReferralTier, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT tier, min_referrals, trunc(min_volume)::text, reward_bps, fee_discount_bps
        FROM perps.referral_tiers ORDER BY min_referrals, min_volume, tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountReferralTier{}
	for rows.Next() {
		row := new(accountReferralTier)
		if err := rows.Scan(&row.Tier, &row.MinReferrals, &row.MinVolume, &row.RewardBps, &row.FeeDiscountBps); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const accountCompetitionSelect = `SELECT id, name, description, start_at, end_at, metric, trunc(prize_pool)::text,
    status = 'active' AND start_at <= CURRENT_TIMESTAMP AND end_at > CURRENT_TIMESTAMP FROM perps.competitions`

func accountScanCompetition(row interface{ Scan(...any) error }, db *sql.DB) (*accountCompetition, error) {
	out := &accountCompetition{db: db}
	var start, end time.Time
	if err := row.Scan(&out.competitionKey, &out.Name, &out.Description, &start, &end, &out.Metric, &out.PrizePool, &out.Active); err != nil {
		return nil, err
	}
	id, startAt, endAt := strconv.FormatInt(out.competitionKey, 10), start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)
	out.ID, out.StartAt, out.EndAt = &id, &startAt, &endAt
	return out, nil
}

func (r *Resolver) Competitions(ctx context.Context) ([]*accountCompetition, error) {
	rows, err := r.DB.QueryContext(ctx, accountCompetitionSelect+` ORDER BY start_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountCompetition{}
	for rows.Next() {
		row, err := accountScanCompetition(rows, r.DB)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Resolver) Competition(ctx context.Context, args struct{ ID string }) (*accountCompetition, error) {
	id, err := strconv.ParseInt(args.ID, 10, 64)
	if err != nil || id < 1 {
		return nil, errors.New("invalid competition id")
	}
	out, err := accountScanCompetition(r.DB.QueryRowContext(ctx, accountCompetitionSelect+` WHERE id = $1`, id), r.DB)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

func (c *accountCompetition) Rankings(ctx context.Context) ([]*accountCompetitionEntry, error) {
	rows, err := c.db.QueryContext(ctx, `WITH scores AS (
        SELECT e.address, CASE c.metric WHEN 'pnl' THEN
            COALESCE((SELECT sum(p.realized_pnl) FROM perps.position_events p WHERE p.account = e.address
                AND p.kind = 'closed' AND p.block_time > c.start_at AND p.block_time <= c.end_at), 0)
            WHEN 'volume' THEN COALESCE((SELECT sum(s.size_usd) FROM perps.trade_settlements s WHERE s.account = e.address
                AND s.block_time > c.start_at AND s.block_time <= c.end_at), 0) END AS score
        FROM perps.competition_entries e JOIN perps.competitions c ON c.id = e.competition_id WHERE c.id = $1
    ) SELECT row_number() OVER (ORDER BY score DESC, address), '0x' || encode(address, 'hex'), trunc(score)::text
        FROM scores ORDER BY score DESC, address`, c.competitionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*accountCompetitionEntry{}
	for rows.Next() {
		row := new(accountCompetitionEntry)
		if err := rows.Scan(&row.Rank, &row.Address, &row.Score); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
