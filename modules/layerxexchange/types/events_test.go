package types

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/gogo/protobuf/gogoproto"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

const eventMaxU128 = "340282366920938463463374607431768211455"
const eventMinI128 = "-" + eventMaxU128
const eventMaxI128 = eventMaxU128

func eventInt(t *testing.T, value string) sdk.Int {
	t.Helper()
	integer, ok := sdk.NewIntFromString(value)
	if !ok {
		t.Fatalf("invalid integer fixture %q", value)
	}
	return integer
}

func freshEvent(message proto.Message) proto.Message {
	return reflect.New(reflect.TypeOf(message).Elem()).Interface().(proto.Message)
}

func eventRoundTrip(t *testing.T, original proto.Message) {
	t.Helper()
	encoded, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded := freshEvent(original)
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("protobuf round trip: got %v, want %v", decoded, original)
	}
	reencoded, err := proto.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("protobuf bytes changed: got %x, want %x", reencoded, encoded)
	}
	event, err := sdk.TypedEventToEvent(original)
	if err != nil {
		t.Fatal(err)
	}
	wantType := "paxprotocol.paxchain.layerxexchange." + reflect.TypeOf(original).Elem().Name()
	if event.Type != wantType {
		t.Fatalf("typed event name: got %q, want %q", event.Type, wantType)
	}
	manager := sdk.NewEventManager()
	if err := manager.EmitTypedEvent(original); err != nil {
		t.Fatal(err)
	}
	emitted := manager.Events()
	if len(emitted) != 1 {
		t.Fatalf("emitted event count: got %d, want 1", len(emitted))
	}
	if !reflect.DeepEqual(event, emitted[0]) {
		t.Fatalf("emitted event: got %v, want %v", emitted[0], event)
	}
	if emitted[0].Type != wantType {
		t.Fatalf("emitted event name: got %q, want %q", emitted[0].Type, wantType)
	}
	parsed, err := sdk.ParseTypedEvent(abci.Event(emitted[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, parsed) {
		t.Fatalf("typed event round trip: got %v, want %v", parsed, original)
	}
}

func TestEventRoundTrip(t *testing.T) {
	maxAmount := eventInt(t, eventMaxU128)
	minimum := eventInt(t, eventMinI128)
	maximum := eventInt(t, eventMaxI128)
	market := strings.Repeat("12", 32)
	order := strings.Repeat("34", 32)
	account := strings.Repeat("56", 32)
	asset := strings.Repeat("78", 32)
	owner := "0x" + strings.Repeat("9a", 20)
	cases := []proto.Message{
		&EventMarginDeposited{DepositId: order, MarginAccount: account, Owner: owner, AssetId: asset, Amount: maxAmount},
		&EventMarginWithdrawn{MarginAccount: account, Owner: owner, AssetId: asset, Amount: maxAmount},
		&EventPerpsMarketCreated{MarketId: market, QuoteAsset: asset},
		&EventPerpsMarketHalted{MarketId: market, Administrator: account, Halted: true},
		&EventOracleAccepted{MarketId: market, OraclePubkey: account, Sequence: math.MaxUint64, Price: maxAmount},
		&EventOrderPlaced{OrderId: order, MarketId: market, FillCount: 8, Quantity: maxAmount},
		&EventOrderCancelled{OrderId: order, MarketId: market},
		&EventPositionOpened{PositionId: account, MarketId: market, Side: 1},
		&EventPositionIncreased{PositionId: account, MarketId: market, Side: 2},
		&EventPositionClosed{PositionId: account, MarketId: market, Owed: minimum},
		&EventFundingSettled{MarketId: market, QuoteAsset: asset, FundingIndex: minimum, Rate: maximum},
		&EventLiquidated{PositionId: account, MarketId: market, Covered: maxAmount},
		&EventDeleveraged{MarketId: market, LiquidityId: account, Count: 16, Deficit: maxAmount},
		&EventSpotMarketCreated{MarketId: market, BaseAsset: account, QuoteAsset: asset},
		&EventSpotOrderPlaced{OrderId: order, MarketId: market, FillCount: 8, Quantity: maxAmount, Remaining: sdk.NewInt(17)},
		&EventSpotOrderCancelled{OrderId: order, MarketId: market, Escrowed: maxAmount},
		&EventSpotMarketHalted{MarketId: market, Administrator: account},
		&EventSpotMarketResumed{MarketId: market, Administrator: account},
	}
	for _, event := range cases {
		t.Run(reflect.TypeOf(event).Elem().Name(), func(t *testing.T) {
			eventRoundTrip(t, event)
		})
	}
}

func TestEventSignedAndZeroRoundTrip(t *testing.T) {
	for _, value := range []string{eventMinI128, "-1", "0", "1", eventMaxI128} {
		t.Run(value, func(t *testing.T) {
			amount := eventInt(t, value)
			eventRoundTrip(t, &EventPositionClosed{PositionId: "position", MarketId: "market", Owed: amount})
			eventRoundTrip(t, &EventFundingSettled{MarketId: "market", QuoteAsset: "quote", FundingIndex: amount, Rate: amount})
		})
	}
	eventRoundTrip(t, &EventSpotOrderPlaced{OrderId: "order", MarketId: "market", FillCount: 0, Quantity: sdk.NewInt(1), Remaining: sdk.ZeroInt()})
	eventRoundTrip(t, &EventPerpsMarketHalted{MarketId: "market", Administrator: "administrator", Halted: false})
}

func TestEventIntentRoundTrip(t *testing.T) {
	cases := []proto.Message{
		&EventMarginWithdrawalRequested{IntentId: "withdrawal", Owner: "owner", Account: "account", AssetId: "asset", Amount: eventMaxU128, Nonce: math.MaxUint64},
		&EventOrderCancelRequested{IntentId: "cancellation", Owner: "owner", OrderId: "order", Nonce: math.MaxUint64},
		&EventSettlementRequested{IntentId: "settlement", Owner: "owner", PositionId: "position", Nonce: math.MaxUint64},
	}
	for _, event := range cases {
		t.Run(reflect.TypeOf(event).Elem().Name(), func(t *testing.T) {
			eventRoundTrip(t, event)
		})
	}
}

func TestEventIntegerAttributes(t *testing.T) {
	cases := []struct {
		message proto.Message
		values  map[string]string
	}{
		{&EventMarginDeposited{Amount: eventInt(t, eventMaxU128)}, map[string]string{"amount": `"` + eventMaxU128 + `"`}},
		{&EventOracleAccepted{Sequence: math.MaxUint64, Price: eventInt(t, eventMaxU128)}, map[string]string{"sequence": `"18446744073709551615"`, "price": `"` + eventMaxU128 + `"`}},
		{&EventPositionClosed{Owed: eventInt(t, eventMinI128)}, map[string]string{"owed": `"` + eventMinI128 + `"`}},
		{&EventFundingSettled{FundingIndex: eventInt(t, eventMinI128), Rate: sdk.NewInt(-1)}, map[string]string{"funding_index": `"` + eventMinI128 + `"`, "rate": `"-1"`}},
		{&EventSpotOrderPlaced{Quantity: eventInt(t, eventMaxU128), Remaining: sdk.NewInt(17)}, map[string]string{"quantity": `"` + eventMaxU128 + `"`, "remaining": `"17"`}},
		{&EventSpotOrderCancelled{Escrowed: eventInt(t, eventMaxU128)}, map[string]string{"escrowed": `"` + eventMaxU128 + `"`}},
	}
	for _, tc := range cases {
		t.Run(reflect.TypeOf(tc.message).Elem().Name(), func(t *testing.T) {
			event, err := sdk.TypedEventToEvent(tc.message)
			if err != nil {
				t.Fatal(err)
			}
			attributes := make(map[string]string)
			for _, attribute := range event.Attributes {
				attributes[string(attribute.Key)] = string(attribute.Value)
			}
			for name, want := range tc.values {
				if got := attributes[name]; got != want {
					t.Errorf("attribute %s: got %q, want %q", name, got, want)
				}
			}
		})
	}
}

type eventFieldSpec struct {
	name    string
	typeID  descriptor.FieldDescriptorProto_Type
	integer bool
}

func TestEventSchema(t *testing.T) {
	stringType := descriptor.FieldDescriptorProto_TYPE_STRING
	uint32Type := descriptor.FieldDescriptorProto_TYPE_UINT32
	uint64Type := descriptor.FieldDescriptorProto_TYPE_UINT64
	boolType := descriptor.FieldDescriptorProto_TYPE_BOOL
	cases := []struct {
		name   string
		fields []eventFieldSpec
	}{
		{"EventMarginDeposited", []eventFieldSpec{{"deposit_id", stringType, false}, {"margin_account", stringType, false}, {"owner", stringType, false}, {"asset_id", stringType, false}, {"amount", stringType, true}}},
		{"EventMarginWithdrawn", []eventFieldSpec{{"margin_account", stringType, false}, {"owner", stringType, false}, {"asset_id", stringType, false}, {"amount", stringType, true}}},
		{"EventPerpsMarketCreated", []eventFieldSpec{{"market_id", stringType, false}, {"quote_asset", stringType, false}}},
		{"EventPerpsMarketHalted", []eventFieldSpec{{"market_id", stringType, false}, {"administrator", stringType, false}, {"halted", boolType, false}}},
		{"EventOracleAccepted", []eventFieldSpec{{"market_id", stringType, false}, {"oracle_pubkey", stringType, false}, {"sequence", uint64Type, false}, {"price", stringType, true}}},
		{"EventOrderPlaced", []eventFieldSpec{{"order_id", stringType, false}, {"market_id", stringType, false}, {"fill_count", uint32Type, false}, {"quantity", stringType, true}}},
		{"EventOrderCancelled", []eventFieldSpec{{"order_id", stringType, false}, {"market_id", stringType, false}}},
		{"EventPositionOpened", []eventFieldSpec{{"position_id", stringType, false}, {"market_id", stringType, false}, {"side", uint32Type, false}}},
		{"EventPositionIncreased", []eventFieldSpec{{"position_id", stringType, false}, {"market_id", stringType, false}, {"side", uint32Type, false}}},
		{"EventPositionClosed", []eventFieldSpec{{"position_id", stringType, false}, {"market_id", stringType, false}, {"owed", stringType, true}}},
		{"EventFundingSettled", []eventFieldSpec{{"market_id", stringType, false}, {"quote_asset", stringType, false}, {"funding_index", stringType, true}, {"rate", stringType, true}}},
		{"EventLiquidated", []eventFieldSpec{{"position_id", stringType, false}, {"market_id", stringType, false}, {"covered", stringType, true}}},
		{"EventDeleveraged", []eventFieldSpec{{"market_id", stringType, false}, {"liquidity_id", stringType, false}, {"count", uint32Type, false}, {"deficit", stringType, true}}},
		{"EventSpotMarketCreated", []eventFieldSpec{{"market_id", stringType, false}, {"base_asset", stringType, false}, {"quote_asset", stringType, false}}},
		{"EventSpotOrderPlaced", []eventFieldSpec{{"order_id", stringType, false}, {"market_id", stringType, false}, {"fill_count", uint32Type, false}, {"quantity", stringType, true}, {"remaining", stringType, true}}},
		{"EventSpotOrderCancelled", []eventFieldSpec{{"order_id", stringType, false}, {"market_id", stringType, false}, {"escrowed", stringType, true}}},
		{"EventSpotMarketHalted", []eventFieldSpec{{"market_id", stringType, false}, {"administrator", stringType, false}}},
		{"EventSpotMarketResumed", []eventFieldSpec{{"market_id", stringType, false}, {"administrator", stringType, false}}},
		{"EventMarginWithdrawalRequested", []eventFieldSpec{{"intent_id", stringType, false}, {"owner", stringType, false}, {"account", stringType, false}, {"asset_id", stringType, false}, {"amount", stringType, false}, {"nonce", uint64Type, false}}},
		{"EventOrderCancelRequested", []eventFieldSpec{{"intent_id", stringType, false}, {"owner", stringType, false}, {"order_id", stringType, false}, {"nonce", uint64Type, false}}},
		{"EventSettlementRequested", []eventFieldSpec{{"intent_id", stringType, false}, {"owner", stringType, false}, {"position_id", stringType, false}, {"nonce", uint64Type, false}}},
	}
	compressed, _ := (&EventMarginDeposited{}).Descriptor()
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	var file descriptor.FileDescriptorProto
	if err := proto.Unmarshal(encoded, &file); err != nil {
		t.Fatal(err)
	}
	if got, want := file.GetPackage(), "paxprotocol.paxchain.layerxexchange"; got != want {
		t.Fatalf("package: got %q, want %q", got, want)
	}
	if len(file.MessageType) != len(cases) {
		t.Fatalf("event count: got %d, want %d", len(file.MessageType), len(cases))
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message := file.MessageType[index]
			if message.GetName() != tc.name {
				t.Fatalf("event %d: got %q, want %q", index, message.GetName(), tc.name)
			}
			if len(message.Field) != len(tc.fields) {
				t.Fatalf("field count: got %d, want %d", len(message.Field), len(tc.fields))
			}
			for fieldIndex, want := range tc.fields {
				field := message.Field[fieldIndex]
				if field.GetName() != want.name || field.GetNumber() != int32(fieldIndex+1) || field.GetType() != want.typeID || field.GetLabel() != descriptor.FieldDescriptorProto_LABEL_OPTIONAL {
					t.Fatalf("field %d: got %v, want name %s, number %d, type %v, optional", fieldIndex, field, want.name, fieldIndex+1, want.typeID)
				}
				if !want.integer {
					if proto.HasExtension(field.GetOptions(), gogoproto.E_Customtype) {
						t.Fatalf("unexpected custom type on %s", want.name)
					}
					continue
				}
				customType, err := proto.GetExtension(field.GetOptions(), gogoproto.E_Customtype)
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := customType.(*string); !ok || *got != "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types.Int" {
					t.Fatalf("custom type on %s: got %v", want.name, customType)
				}
				nullable, err := proto.GetExtension(field.GetOptions(), gogoproto.E_Nullable)
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := nullable.(*bool); !ok || *got {
					t.Fatalf("nullable integer field %s: got %v", want.name, nullable)
				}
			}
		})
	}
	for _, name := range []string{"EventMarginWithdrawalRequested", "EventOrderCancelRequested", "EventSettlementRequested"} {
		if proto.MessageType(file.GetPackage()+"."+name) == nil {
			t.Errorf("intent event is not registered: %s", name)
		}
	}
}

func TestEventIntegerRefusals(t *testing.T) {
	fields := []struct {
		message proto.Message
		number  byte
		name    string
	}{
		{&EventMarginDeposited{}, 5, "Amount"},
		{&EventMarginWithdrawn{}, 4, "Amount"},
		{&EventOracleAccepted{}, 4, "Price"},
		{&EventOrderPlaced{}, 4, "Quantity"},
		{&EventPositionClosed{}, 3, "Owed"},
		{&EventFundingSettled{}, 3, "FundingIndex"},
		{&EventFundingSettled{}, 4, "Rate"},
		{&EventLiquidated{}, 3, "Covered"},
		{&EventDeleveraged{}, 4, "Deficit"},
		{&EventSpotOrderPlaced{}, 4, "Quantity"},
		{&EventSpotOrderPlaced{}, 5, "Remaining"},
		{&EventSpotOrderCancelled{}, 3, "Escrowed"},
	}
	cases := []struct {
		name  string
		value string
		error string
	}{
		{"malformed", "invalid", `math/big: cannot unmarshal "invalid" into a *big.Int`},
		{"too_long", strings.Repeat("1", 101), "integer string too long: got 101, max 100"},
		{"overflow", "115792089237316195423570985008687907853269984665640564039457584007913129639936", "integer out of range; got: 257, max: 256"},
	}
	for _, field := range fields {
		t.Run(reflect.TypeOf(field.message).Elem().Name()+"/"+field.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					wire := append([]byte{field.number<<3 | 2, byte(len(tc.value))}, []byte(tc.value)...)
					err := proto.Unmarshal(wire, freshEvent(field.message))
					if err == nil || err.Error() != tc.error {
						t.Fatalf("got %v, want %q", err, tc.error)
					}
				})
			}
			t.Run("wrong_wire", func(t *testing.T) {
				err := proto.Unmarshal([]byte{field.number << 3, 1}, freshEvent(field.message))
				want := fmt.Sprintf("proto: wrong wireType = 0 for field %s", field.name)
				if err == nil || err.Error() != want {
					t.Fatalf("got %v, want %q", err, want)
				}
			})
		})
	}
}
