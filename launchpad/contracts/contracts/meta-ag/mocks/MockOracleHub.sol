// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/**
 * @title MockOracleHub
 * @notice Minimal IOracleHub spy that returns configured prices and
 *         availability per token. Used by MetaAGRouter unit tests to
 *         exercise the S4 sanity-check branches in isolation, without
 *         spinning up the full PriceOracle → PriceOracleAdapter → OracleHub
 *         composition.
 * @dev Scope-isolated under `contracts/meta-ag/mocks/`. Only implements
 *      the two functions MetaAGRouter._oracleSanityCheck consumes:
 *        - isPriceAvailable(token) → (bool, uint256 confidence)
 *        - getPrice(token) → uint256 price (18 decimals)
 *
 *      The mock has no upstream relayer / staleness logic — tests drive it
 *      directly via setPrice(token, price) and setAvailable(token, bool).
 */
contract MockOracleHub {
    mapping(address => uint256) private _prices;
    mapping(address => bool) private _available;

    /// @notice Default confidence returned alongside isPriceAvailable.
    uint256 public constant CONFIDENCE = 5000;

    /// @notice Set both price and availability in one call. Use 0 + false
    ///         to clear a token's state.
    function setPrice(address token, uint256 price) external {
        _prices[token] = price;
        _available[token] = price != 0;
    }

    /// @notice Override availability without touching price (tests that need
    ///         to simulate the "price = X but adapter says not available" path).
    function setAvailable(address token, bool flag) external {
        _available[token] = flag;
    }

    function isPriceAvailable(
        address token
    ) external view returns (bool available, uint256 bestConfidence) {
        return (_available[token], _available[token] ? CONFIDENCE : 0);
    }

    function getPrice(address token) external view returns (uint256) {
        return _prices[token];
    }
}
