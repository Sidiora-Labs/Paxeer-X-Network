package holdings

import (
	"testing"
)

func TestHoldingsNumericSchemaAndOrdering(t *testing.T) {
	ctx, r := phase2Repository(t)
	token := addr(1)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	for i, amount := range []string{"9", "100", "20", "20"} {
		applyFixture(t, ctx, r, event(token, Zero, addr(i+2), amount, i))
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Balances) != 4 || s.Balances[0].Raw != "100" || s.Balances[1].Raw != "20" || s.Balances[1].Address != addr(4) || s.Balances[2].Address != addr(5) || s.Balances[3].Raw != "9" {
		t.Fatal(s)
	}
	for _, raw := range []string{"-1", "0", "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		if _, err = r.pool.Exec(ctx, "INSERT INTO "+r.schema+".balances(token,address,balance) VALUES($1,$2,$3)", token, addr(99), raw); err == nil {
			t.Fatal("numeric constraint admitted", raw)
		}
	}
	var dataType string
	if err = r.pool.QueryRow(ctx, "SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name='balances' AND column_name='balance'", r.schema[1:len(r.schema)-1]).Scan(&dataType); err != nil || dataType != "numeric" {
		t.Fatal(dataType, err)
	}
}
