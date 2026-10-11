package precompiles

import (
	"fmt"
	"sync"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/bank"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/distribution"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/feetoken"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/gov"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/ibc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/json"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxanchor"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxbridge"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxcustody"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxexchange"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxverify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/oracle"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/p256"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pointer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pointerview"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/solo"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/staking"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/wasmd"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"golang.org/x/mod/semver"
)

// XWebUpgrade is the upgrade that brings the xweb precompile. A custom
// precompile set built for an earlier latest upgrade carries no entry at
// xweb.XWebAddress.
const XWebUpgrade = "v6.8"

// FeeTokenUpgrade is the upgrade that brings the fee-token precompile. A custom
// precompile set built for an earlier latest upgrade carries no entry at
// feetoken.FeeTokenAddress, and the entry carries a version at this upgrade so
// that execution below its height leaves the precompile out.
const FeeTokenUpgrade = "v6.7"

// MergeUpgrade is the plan that merges the LayerX kernel into the chain and
// brings the MergeAddressTable precompiles online. It equals the plan name the
// node registers, and the merge entries carry a version at this plan so that
// the keeper's activation entry for the plan withholds them below its height.
const MergeUpgrade = "v7.0"

// MergeAddress is one precompile address the merge plan brings online and the
// name its precompile reports through GetName.
type MergeAddress struct {
	Address ecommon.Address
	Name    string
}

// MergeAddressTable lists the precompiles the merge plan brings online: the
// four construct addresses and the eight agent-function addresses.
var MergeAddressTable = []MergeAddress{
	{ecommon.HexToAddress("0x000000000000000000000000000000000000101A"), "layerxledger"},
	{ecommon.HexToAddress("0x000000000000000000000000000000000000101B"), "layerxpay"},
	{ecommon.HexToAddress("0x000000000000000000000000000000000000101C"), "layerxauth"},
	{ecommon.HexToAddress("0x000000000000000000000000000000000000101D"), "layerxprograms"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000901"), "orob"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000902"), "clearing"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000903"), "oracle aggregator"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000904"), "pofq"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000905"), "scheduler"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000906"), "streams"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000907"), "tee attestor"},
	{ecommon.HexToAddress("0x0000000000000000000000000000000000000908"), "eip712"},
}

// MergeConstructor builds the precompile served at one MergeAddressTable
// address from the precompile keepers and the merge plan name.
type MergeConstructor func(keepers utils.Keepers, planName string) (IPrecompile, error)

var (
	mergeConstructorsMtx sync.RWMutex
	mergeConstructors    = map[ecommon.Address]MergeConstructor{}
)

// RegisterMergeConstructor records the constructor of the precompile served at
// a MergeAddressTable address. An address with no registered constructor is
// not served. It refuses an address outside the table, a nil constructor and
// a second constructor for the same address.
func RegisterMergeConstructor(address ecommon.Address, constructor MergeConstructor) error {
	if _, ok := mergeAddressName(address); !ok {
		return fmt.Errorf("merge constructor for %s outside the merge address table", address.Hex())
	}
	if constructor == nil {
		return fmt.Errorf("nil merge constructor for %s", address.Hex())
	}
	mergeConstructorsMtx.Lock()
	defer mergeConstructorsMtx.Unlock()
	if _, registered := mergeConstructors[address]; registered {
		return fmt.Errorf("merge constructor for %s already registered", address.Hex())
	}
	mergeConstructors[address] = constructor
	return nil
}

func mergeAddressName(address ecommon.Address) (string, bool) {
	for _, entry := range MergeAddressTable {
		if entry.Address == address {
			return entry.Name, true
		}
	}
	return "", false
}

// newMergePrecompiles builds, in MergeAddressTable order, the precompile of
// every address with a registered constructor, and refuses a precompile whose
// address or name differs from its table entry.
func newMergePrecompiles(keepers utils.Keepers) ([]IPrecompile, error) {
	mergeConstructorsMtx.RLock()
	defer mergeConstructorsMtx.RUnlock()
	built := make([]IPrecompile, 0, len(mergeConstructors))
	for _, entry := range MergeAddressTable {
		constructor, registered := mergeConstructors[entry.Address]
		if !registered {
			continue
		}
		p, err := constructor(keepers, MergeUpgrade)
		if err != nil {
			return nil, fmt.Errorf("merge precompile %s at %s: %w", entry.Name, entry.Address.Hex(), err)
		}
		if p == nil {
			return nil, fmt.Errorf("merge precompile %s at %s: constructor returned no precompile", entry.Name, entry.Address.Hex())
		}
		if p.Address() != entry.Address || p.GetName() != entry.Name {
			return nil, fmt.Errorf("merge precompile %s at %s: constructor built %s at %s",
				entry.Name, entry.Address.Hex(), p.GetName(), p.Address().Hex())
		}
		built = append(built, p)
	}
	return built, nil
}

var SetupMtx = &sync.Mutex{}
var Initialized = false

type PrecompileInfo struct {
	ABI     abi.ABI
	Address ecommon.Address
}

// PrecompileNamesToInfo is Populated by InitializePrecompiles
var PrecompileNamesToInfo = map[string]PrecompileInfo{}

type IPrecompile interface {
	vm.PrecompiledContract
	GetABI() abi.ABI
	GetName() string
	Address() ecommon.Address
}

func GetCustomPrecompiles(
	latestUpgrade string,
	keepers utils.Keepers,
) map[ecommon.Address]utils.VersionedPrecompiles {
	custom := map[ecommon.Address]utils.VersionedPrecompiles{
		ecommon.HexToAddress(bank.BankAddress):                   bank.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(wasmd.WasmdAddress):                 wasmd.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(json.JSONAddress):                   json.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(addr.AddrAddress):                   addr.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(staking.StakingAddress):             staking.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(gov.GovAddress):                     gov.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(distribution.DistrAddress):          distribution.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(oracle.OracleAddress):               oracle.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(ibc.IBCAddress):                     ibc.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(pointer.PointerAddress):             pointer.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(pointerview.PointerViewAddress):     pointerview.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(p256.P256VerifyAddress):             p256.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(solo.SoloAddress):                   solo.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(layerxverify.LayerXVerifyAddress):   layerxverify.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(layerxcustody.LayerXCustodyAddress): layerxcustody.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(layerxanchor.LayerXAnchorAddress):   layerxanchor.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(layerxexchange.ExchangeAddress):     layerxexchange.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(layerxbridge.BridgeAddress):         layerxbridge.GetVersioned(latestUpgrade, keepers),
		ecommon.HexToAddress(launchpad.LaunchpadAddress):         launchpad.GetVersioned(latestUpgrade, keepers),
	}
	if semver.Compare(latestUpgrade, FeeTokenUpgrade) >= 0 {
		versioned := feetoken.GetVersioned(latestUpgrade, keepers)
		versioned[FeeTokenUpgrade] = versioned[latestUpgrade]
		custom[ecommon.HexToAddress(feetoken.FeeTokenAddress)] = versioned
	}
	if semver.Compare(latestUpgrade, XWebUpgrade) >= 0 {
		custom[ecommon.HexToAddress(xweb.XWebAddress)] = xweb.GetVersioned(latestUpgrade, keepers)
	}
	merge, err := newMergePrecompiles(keepers)
	if err != nil {
		panic(err)
	}
	for _, p := range merge {
		custom[p.Address()] = utils.VersionedPrecompiles{latestUpgrade: p, MergeUpgrade: p}
	}
	return custom
}

func InitializePrecompiles(
	dryRun bool,
	keepers utils.Keepers,
) error {
	SetupMtx.Lock()
	defer SetupMtx.Unlock()
	if Initialized {
		panic("precompiles already initialized")
	}
	bankp, err := bank.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	wasmdp, err := wasmd.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	jsonp, err := json.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	addrp, err := addr.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	stakingp, err := staking.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	govp, err := gov.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	distrp, err := distribution.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	oraclep, err := oracle.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	ibcp, err := ibc.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	pointerp, err := pointer.NewPrecompile(keepers)
	if err != nil {
		return err
	}
	pointerviewp, err := pointerview.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	p256p, err := p256.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	layerxverifyp, err := layerxverify.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	layerxanchorp, err := layerxanchor.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	layerxcustodyp, err := layerxcustody.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	layerxexchangep, err := layerxexchange.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	// A dry run only records names and ABIs, so it builds the bridge and
	// launchpad precompiles without their keepers; a live run requires them.
	layerxbridgep, err := layerxbridge.NewPrecompile(keepers)
	if err != nil {
		if !dryRun {
			return err
		}
		layerxbridgep = layerxbridge.NewPrecompileWithKeeper(nil)
	}

	launchpadp, err := launchpad.NewPrecompile(keepers)
	if err != nil {
		if !dryRun {
			return err
		}
		if launchpadp, err = launchpad.NewPrecompileWithKeeper(nil, keepers.EVMK()); err != nil {
			return err
		}
	}

	feetokenp, err := feetoken.NewPrecompile(keepers)
	if err != nil {
		if !dryRun {
			return err
		}
		feetokenp = feetoken.NewPrecompileWithKeeper(nil)
	}

	xwebp, err := xweb.NewPrecompile(keepers)
	if err != nil {
		return err
	}

	PrecompileNamesToInfo[feetokenp.GetName()] = PrecompileInfo{ABI: feetokenp.GetABI(), Address: feetokenp.Address()}
	PrecompileNamesToInfo[bankp.GetName()] = PrecompileInfo{ABI: bankp.GetABI(), Address: bankp.Address()}
	PrecompileNamesToInfo[wasmdp.GetName()] = PrecompileInfo{ABI: wasmdp.GetABI(), Address: wasmdp.Address()}
	PrecompileNamesToInfo[jsonp.GetName()] = PrecompileInfo{ABI: jsonp.GetABI(), Address: jsonp.Address()}
	PrecompileNamesToInfo[addrp.GetName()] = PrecompileInfo{ABI: addrp.GetABI(), Address: addrp.Address()}
	PrecompileNamesToInfo[stakingp.GetName()] = PrecompileInfo{ABI: stakingp.GetABI(), Address: stakingp.Address()}
	PrecompileNamesToInfo[govp.GetName()] = PrecompileInfo{ABI: govp.GetABI(), Address: govp.Address()}
	PrecompileNamesToInfo[distrp.GetName()] = PrecompileInfo{ABI: distrp.GetABI(), Address: distrp.Address()}
	PrecompileNamesToInfo[oraclep.GetName()] = PrecompileInfo{ABI: oraclep.GetABI(), Address: oraclep.Address()}
	PrecompileNamesToInfo[ibcp.GetName()] = PrecompileInfo{ABI: ibcp.GetABI(), Address: ibcp.Address()}
	PrecompileNamesToInfo[pointerp.GetName()] = PrecompileInfo{ABI: pointerp.GetABI(), Address: pointerp.Address()}
	PrecompileNamesToInfo[pointerviewp.GetName()] = PrecompileInfo{ABI: pointerviewp.GetABI(), Address: pointerviewp.Address()}
	PrecompileNamesToInfo[p256p.GetName()] = PrecompileInfo{ABI: p256p.GetABI(), Address: p256p.Address()}
	PrecompileNamesToInfo[layerxverifyp.GetName()] = PrecompileInfo{ABI: layerxverifyp.GetABI(), Address: layerxverifyp.Address()}
	PrecompileNamesToInfo[layerxcustodyp.GetName()] = PrecompileInfo{ABI: layerxcustodyp.GetABI(), Address: layerxcustodyp.Address()}
	PrecompileNamesToInfo[layerxanchorp.GetName()] = PrecompileInfo{ABI: layerxanchorp.GetABI(), Address: layerxanchorp.Address()}
	PrecompileNamesToInfo[layerxexchangep.GetName()] = PrecompileInfo{ABI: layerxexchangep.GetABI(), Address: layerxexchangep.Address()}
	PrecompileNamesToInfo[layerxbridgep.GetName()] = PrecompileInfo{ABI: layerxbridgep.GetABI(), Address: layerxbridgep.Address()}
	PrecompileNamesToInfo[launchpadp.GetName()] = PrecompileInfo{ABI: launchpadp.GetABI(), Address: launchpadp.Address()}
	PrecompileNamesToInfo[xwebp.GetName()] = PrecompileInfo{ABI: xwebp.GetABI(), Address: xwebp.Address()}

	merge, err := newMergePrecompiles(keepers)
	if err != nil {
		return err
	}
	for _, p := range merge {
		PrecompileNamesToInfo[p.GetName()] = PrecompileInfo{ABI: p.GetABI(), Address: p.Address()}
	}

	if !dryRun {
		addPrecompileToVM(feetokenp)
		addPrecompileToVM(bankp)
		addPrecompileToVM(wasmdp)
		addPrecompileToVM(jsonp)
		addPrecompileToVM(addrp)
		addPrecompileToVM(stakingp)
		addPrecompileToVM(govp)
		addPrecompileToVM(distrp)
		addPrecompileToVM(oraclep)
		addPrecompileToVM(ibcp)
		addPrecompileToVM(pointerp)
		addPrecompileToVM(pointerviewp)
		addPrecompileToVM(p256p)
		addPrecompileToVM(layerxverifyp)
		addPrecompileToVM(layerxcustodyp)
		addPrecompileToVM(layerxanchorp)
		addPrecompileToVM(layerxexchangep)
		addPrecompileToVM(layerxbridgep)
		addPrecompileToVM(launchpadp)
		addPrecompileToVM(xwebp)
		for _, p := range merge {
			addPrecompileToVM(p)
		}
		Initialized = true
	}
	return nil
}

func GetPrecompileInfo(name string) PrecompileInfo {
	if !Initialized {
		// Precompile Info does not require any keeper state
		_ = InitializePrecompiles(true, &utils.EmptyKeepers{})
	}
	i, ok := PrecompileNamesToInfo[name]
	if !ok {
		panic(name + "doesn't exist as a precompile")
	}
	return i
}

// This function modifies global variable in `vm` module. It should only be called once
// per precompile during initialization
func addPrecompileToVM(p IPrecompile) {
	vm.PrecompiledContractsHomestead[p.Address()] = p
	vm.PrecompiledContractsByzantium[p.Address()] = p
	vm.PrecompiledContractsIstanbul[p.Address()] = p
	vm.PrecompiledContractsBerlin[p.Address()] = p
	vm.PrecompiledContractsCancun[p.Address()] = p
	vm.PrecompiledContractsBLS[p.Address()] = p
	vm.PrecompiledAddressesHomestead = append(vm.PrecompiledAddressesHomestead, p.Address())
	vm.PrecompiledAddressesByzantium = append(vm.PrecompiledAddressesByzantium, p.Address())
	vm.PrecompiledAddressesIstanbul = append(vm.PrecompiledAddressesIstanbul, p.Address())
	vm.PrecompiledAddressesBerlin = append(vm.PrecompiledAddressesBerlin, p.Address())
	vm.PrecompiledAddressesCancun = append(vm.PrecompiledAddressesCancun, p.Address())
}

var PrecompileLastUpgrade = map[string]int64{
	bank.BankAddress: 1,
}
