package types

import (
	"fmt"
	launchpad "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	anchor "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	bridge "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	custody "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	exchange "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	web "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	cdctypes "github.com/sidiora-labs/paxeer-network/sdk/codec/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	authtypes "github.com/sidiora-labs/paxeer-network/sdk/x/auth/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	"reflect"
	"strings"
)

const ProposalTypeLayerX = "LayerX"

var _ govtypes.Content = &LayerXProposal{}
var _ cdctypes.UnpackInterfacesMessage = &LayerXProposal{}

func init() {
	govtypes.RegisterProposalType(ProposalTypeLayerX)
	govtypes.RegisterProposalTypeCodec(&LayerXProposal{}, "layerxgov/LayerXProposal")
	govtypes.RegisterProposalTypeCodec(&anchor.MsgUpdateParams{}, "layerxanchor/MsgUpdateParams")
	govtypes.RegisterProposalTypeCodec(&exchange.MsgUpdateParams{}, "layerxexchange/MsgUpdateParams")
	govtypes.RegisterProposalTypeCodec(&exchange.MsgSetMarket{}, "layerxexchange/MsgSetMarket")
	govtypes.RegisterProposalTypeCodec(&launchpad.MsgUpdateParams{}, "launchpad/MsgUpdateParams")
	govtypes.RegisterProposalTypeCodec(&web.MsgRegisterAttestor{}, "xweb/MsgRegisterAttestor")
	govtypes.RegisterProposalTypeCodec(&web.MsgRemoveAttestor{}, "xweb/MsgRemoveAttestor")
	govtypes.RegisterProposalTypeCodec(&web.MsgSetThreshold{}, "xweb/MsgSetThreshold")
	govtypes.RegisterProposalTypeCodec(&web.MsgSetParams{}, "xweb/MsgSetParams")
	govtypes.RegisterProposalTypeCodec(&web.MsgPause{}, "xweb/MsgPause")
	govtypes.RegisterProposalTypeCodec(&web.MsgUnpause{}, "xweb/MsgUnpause")
}
func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&LayerXProposal{}, "layerxgov/LayerXProposal", nil)
}
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*govtypes.Content)(nil), &LayerXProposal{})
}
func GovernanceAuthority() string { return authtypes.NewModuleAddress(govtypes.ModuleName).String() }
func NewLayerXProposal(title, description string, msgs ...sdk.Msg) (*LayerXProposal, error) {
	packed := make([]*cdctypes.Any, 0, len(msgs))
	for _, msg := range msgs {
		if _, err := governanceMessage(msg); err != nil {
			return nil, err
		}
		value, err := cdctypes.NewAnyWithValue(msg)
		if err != nil {
			return nil, err
		}
		packed = append(packed, value)
	}
	return &LayerXProposal{Title: title, Description: description, Messages: packed}, nil
}
func (p *LayerXProposal) GetTitle() string       { return p.Title }
func (p *LayerXProposal) GetDescription() string { return p.Description }
func (p *LayerXProposal) ProposalRoute() string  { return RouterKey }
func (p *LayerXProposal) ProposalType() string   { return ProposalTypeLayerX }
func (p *LayerXProposal) ValidateBasic() error {
	if p == nil {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "nil proposal")
	}
	if err := govtypes.ValidateAbstract(p); err != nil {
		return err
	}
	msgs, err := p.GetMessages()
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "the proposal carries no message")
	}
	for _, msg := range msgs {
		if err := msg.ValidateBasic(); err != nil {
			return err
		}
		authority, err := governanceMessage(msg)
		if err != nil {
			return err
		}
		if authority != GovernanceAuthority() {
			return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "message authority is not the governance module account")
		}
	}
	return nil
}
func (p *LayerXProposal) GetMessages() ([]sdk.Msg, error) {
	if p == nil {
		return nil, sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "nil proposal")
	}
	msgs := make([]sdk.Msg, 0, len(p.Messages))
	for _, packed := range p.Messages {
		if packed == nil {
			return nil, sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "empty message")
		}
		msg, ok := packed.GetCachedValue().(sdk.Msg)
		if !ok {
			return nil, sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "message is not unpacked")
		}
		if _, err := governanceMessage(msg); err != nil {
			return nil, err
		}
		if packed.TypeUrl != sdk.MsgTypeURL(msg) {
			return nil, sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "message type URL mismatch")
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
func (p LayerXProposal) UnpackInterfaces(unpacker cdctypes.AnyUnpacker) error {
	for _, packed := range p.Messages {
		if packed == nil {
			return sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "empty message")
		}
		var msg sdk.Msg
		if err := unpacker.UnpackAny(packed, &msg); err != nil {
			return err
		}
	}
	return nil
}
func (p LayerXProposal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "LayerX Proposal: %s\n%s\n", p.Title, p.Description)
	for _, msg := range p.Messages {
		if msg != nil {
			fmt.Fprintln(&b, msg.TypeUrl)
		}
	}
	return b.String()
}
func governanceMessage(msg sdk.Msg) (string, error) {
	if msg == nil || (reflect.ValueOf(msg).Kind() == reflect.Ptr && reflect.ValueOf(msg).IsNil()) {
		return "", sdkerrors.Wrap(govtypes.ErrInvalidProposalContent, "nil message")
	}
	switch m := msg.(type) {
	case *custody.MsgUpdateParams:
		return m.Authority, nil
	case *custody.MsgSetAsset:
		return m.Authority, nil
	case *custody.MsgRegisterCheckpoint:
		return m.Authority, nil
	case *custody.MsgSetEmergency:
		return m.Authority, nil
	case *custody.MsgCancelClaim:
		return m.Authority, nil
	case *anchor.MsgUpdateParams:
		return m.Authority, nil
	case *exchange.MsgUpdateParams:
		return m.Authority, nil
	case *exchange.MsgSetMarket:
		return m.Authority, nil
	case *bridge.MsgRegisterChain:
		return m.Authority, nil
	case *bridge.MsgSetAttestors:
		return m.Authority, nil
	case *bridge.MsgSetCap:
		return m.Authority, nil
	case *bridge.MsgPause:
		return m.Authority, nil
	case *bridge.MsgUnpause:
		return m.Authority, nil
	case *bridge.MsgRegisterSidioraPair:
		return m.Authority, nil
	case *launchpad.MsgUpdateParams:
		return m.Authority, nil
	case *web.MsgRegisterAttestor:
		return m.Authority, nil
	case *web.MsgRemoveAttestor:
		return m.Authority, nil
	case *web.MsgSetThreshold:
		return m.Authority, nil
	case *web.MsgSetParams:
		return m.Authority, nil
	case *web.MsgPause:
		return m.Authority, nil
	case *web.MsgUnpause:
		return m.Authority, nil
	default:
		return "", sdkerrors.Wrapf(govtypes.ErrInvalidProposalContent, "%T is not a fork authority message", msg)
	}
}
