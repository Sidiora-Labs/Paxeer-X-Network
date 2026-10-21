package holdings

import (
	"errors"
	"math/big"
)

// DistributionV1 freezes ten raw-balance bands at powers of ten. It is a
// streaming arithmetic primitive; no claim of deployed materialization is made.
const DistributionPolicy = "raw_powers10_v1"

type Band struct {
	Count uint64 `json:"count"`
	Raw   string `json:"raw"`
}
type Denominator struct {
	Raw                string   `json:"raw"`
	Policy             string   `json:"policy"`
	ExcludedCategories []string `json:"excludedCategories"`
}
type Distribution struct {
	InclusiveDenominator   Denominator `json:"inclusiveDenominator"`
	CirculatingDenominator Denominator `json:"circulatingDenominator"`
	CirculatingTop10       Ratio       `json:"circulatingTop10"`
	CirculatingCreator     Ratio       `json:"circulatingCreator"`
	Policy                 string      `json:"policy"`
	AllPositiveCount       uint64      `json:"allPositiveCount"`
	ExternalWalletCount    uint64      `json:"externalWalletCount"`
	UnknownCount           uint64      `json:"unknownCount"`
	Supply                 string      `json:"supply"`
	CirculatingRaw         string      `json:"circulatingRaw"`
	Bands                  [10]Band    `json:"bands"`
	Top10                  Ratio       `json:"top10"`
	Creator                Ratio       `json:"creator"`
}

// Accumulator holds only ten leaders and ten bands, regardless of population.
// Add enforces strictly ascending normalized nonzero addresses from one watermark.
type Accumulator struct {
	result                      Distribution
	total, circulating, creator *big.Int
	top                         []*big.Int
	circulatingTop              []*big.Int
	circulatingCreator          *big.Int
	supply                      string
	lastAddress                 string
}

func NewAccumulator(supply string) (*Accumulator, error) {
	if _, err := UInt(supply); err != nil {
		return nil, err
	}
	a := &Accumulator{result: Distribution{Policy: DistributionPolicy, Supply: supply}, total: new(big.Int), circulating: new(big.Int), creator: new(big.Int), supply: supply, circulatingCreator: new(big.Int)}
	for i := range a.result.Bands {
		a.result.Bands[i].Raw = "0"
	}
	return a, nil
}
func (a *Accumulator) Add(b Balance, isCreator bool) error {
	address, err := Address(b.Address)
	if err != nil {
		return err
	}
	if address == Zero || address <= a.lastAddress {
		return errors.New("distribution requires unique nonzero address sorted stream")
	}
	v, err := UInt(b.Raw)
	if err != nil {
		return err
	}
	switch b.Category {
	case "unknown", "external_wallet", "pool_reserve", "burn", "treasury", "router":
	default:
		return errors.New("invalid category")
	}
	if v.Sign() == 0 {
		return errors.New("distribution requires positive balance")
	}
	a.lastAddress = address
	a.result.AllPositiveCount++
	a.total.Add(a.total, v)
	if isCreator {
		a.creator.Add(a.creator, v)
	}
	switch b.Category {
	case "external_wallet":
		a.result.ExternalWalletCount++
		a.circulating.Add(a.circulating, v)
		a.circulatingTop = addLeader(a.circulatingTop, v)
		if isCreator {
			a.circulatingCreator.Add(a.circulatingCreator, v)
		}
	case "unknown":
		a.result.UnknownCount++
	}
	band := len(v.String()) - 1
	if band > 9 {
		band = 9
	}
	a.result.Bands[band].Count++
	sum, _ := new(big.Int).SetString(a.result.Bands[band].Raw, 10)
	a.result.Bands[band].Raw = sum.Add(sum, v).String()
	a.top = addLeader(a.top, v)
	return nil
}
func addLeader(top []*big.Int, v *big.Int) []*big.Int {
	i := 0
	for i < len(top) && top[i].Cmp(v) >= 0 {
		i++
	}
	if i < 10 {
		top = append(top, nil)
		copy(top[i+1:], top[i:])
		top[i] = new(big.Int).Set(v)
		if len(top) > 10 {
			top = top[:10]
		}
	}
	return top
}
func (a *Accumulator) Finish() (Distribution, error) {
	r := a.result
	if a.total.String() != a.supply {
		return r, ErrIntegrity
	}
	top := new(big.Int)
	for _, v := range a.top {
		top.Add(top, v)
	}
	r.Top10 = BasisPoints(top.String(), a.supply)
	r.Creator = BasisPoints(a.creator.String(), a.supply)
	r.CirculatingRaw = a.circulating.String()
	r.InclusiveDenominator = Denominator{Raw: a.supply, Policy: "indexed_supply_v1", ExcludedCategories: []string{}}
	r.CirculatingDenominator = Denominator{Raw: r.CirculatingRaw, Policy: "verified_external_wallets_v1", ExcludedCategories: []string{"unknown", "pool_reserve", "burn", "treasury", "router"}}
	circulatingTop := new(big.Int)
	for _, v := range a.circulatingTop {
		circulatingTop.Add(circulatingTop, v)
	}
	r.CirculatingTop10 = BasisPoints(circulatingTop.String(), r.CirculatingRaw)
	r.CirculatingCreator = BasisPoints(a.circulatingCreator.String(), r.CirculatingRaw)
	return r, nil
}
