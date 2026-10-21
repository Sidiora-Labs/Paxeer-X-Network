// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";

/// @title TaxOptical
/// @notice Applies additional configurable buy/sell tax via beforeSwap amountDelta.
/// @dev Uses beforeSwap hook. Tax is taken by reducing the effective amountIn
///      (negative amountDelta). The taxed portion remains in the pool, effectively
///      benefiting liquidity. Configurable separate buy/sell rates, capped at 10%.
///      Immutable once deployed — rates are set at construction.
contract TaxOptical is BaseOptical {
    error TaxTooHigh();

    /// @notice Maximum allowed tax in basis points (1000 = 10%)
    uint256 public constant MAX_TAX_BPS = 1000;

    /// @notice Buy tax in basis points (e.g., 100 = 1%)
    uint256 public immutable buyTaxBps;

    /// @notice Sell tax in basis points (e.g., 100 = 1%)
    uint256 public immutable sellTaxBps;

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _buyTaxBps Buy tax in basis points (max 1000 = 10%)
    /// @param _sellTaxBps Sell tax in basis points (max 1000 = 10%)
    constructor(
        address _poolRegistry,
        address _owner,
        uint256 _buyTaxBps,
        uint256 _sellTaxBps
    ) BaseOptical(_poolRegistry, _owner) {
        if (_buyTaxBps > MAX_TAX_BPS) revert TaxTooHigh();
        if (_sellTaxBps > MAX_TAX_BPS) revert TaxTooHigh();
        buyTaxBps = _buyTaxBps;
        sellTaxBps = _sellTaxBps;
    }

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        return BitFlag.BEFORE_SWAP;
    }

    /// @inheritdoc IOptical
    function beforeSwap(
        address pool, /* pool */
        address, /* sender */
        bool isBuy,
        uint256 amountIn
    ) external view override onlyAttachedPool(pool) returns (bool proceed, int256 amountDelta) {
        uint256 taxBps = isBuy ? buyTaxBps : sellTaxBps;

        if (taxBps == 0) {
            return (true, 0);
        }

        // Calculate tax amount and return as negative delta
        uint256 taxAmount = (amountIn * taxBps) / 10000;
        return (true, -int256(taxAmount));
    }
}
