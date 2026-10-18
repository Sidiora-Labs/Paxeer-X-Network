// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {SidioraMath} from "../../libraries/SidioraMath.sol";

/// @notice Exact decimal-normalized rational conversion without overflowing a
/// representable result. Prices are positive; supported token decimals are 0..36.
library PECORMath {
    error InvalidPriceOrDecimals();
    error QuoteOverflow();

    function convert(uint256 amount, uint256 priceIn, uint256 priceOut, uint8 dIn, uint8 dOut, bool up)
        internal pure returns (uint256 result)
    {
        if (priceIn == 0 || priceOut == 0 || dIn > 36 || dOut > 36) revert InvalidPriceOrDecimals();
        if (dOut >= dIn) {
            uint256 scale = 10 ** uint256(dOut - dIn);
            uint256 quotient = SidioraMath.mulDiv(amount, priceIn, priceOut);
            uint256 remainder = mulmod(amount, priceIn, priceOut);
            result = quotient * scale + SidioraMath.mulDiv(remainder, scale, priceOut);
            if (up && mulmod(remainder, scale, priceOut) != 0) ++result;
        } else {
            uint256 scale = 10 ** uint256(dIn - dOut);
            if (priceOut <= type(uint256).max / scale) {
                uint256 denominator = priceOut * scale;
                result = SidioraMath.mulDiv(amount, priceIn, denominator);
                if (up && mulmod(amount, priceIn, denominator) != 0) ++result;
            } else {
                // Only the extreme denominator case uses long division. Both
                // numerator and denominator remain exact 512-bit integers.
                (uint256 nHi, uint256 nLo) = _multiply(amount, priceIn);
                (uint256 dHi, uint256 dLo) = _multiply(priceOut, scale);
                uint256 rHi;
                uint256 rLo;
                for (uint256 bit = 512; bit != 0;) {
                    unchecked { --bit; }
                    rHi = (rHi << 1) | (rLo >> 255);
                    rLo = (rLo << 1) | (bit >= 256 ? (nHi >> (bit - 256)) & 1 : (nLo >> bit) & 1);
                    if (rHi > dHi || (rHi == dHi && rLo >= dLo)) {
                        unchecked {
                            rHi = rHi - dHi - (rLo < dLo ? 1 : 0);
                            rLo -= dLo;
                        }
                        if (bit >= 256) revert QuoteOverflow();
                        result |= uint256(1) << bit;
                    }
                }
                if (up && (rHi != 0 || rLo != 0)) ++result;
            }
        }
    }

    function _multiply(uint256 a, uint256 b) private pure returns (uint256 hi, uint256 lo) {
        assembly ("memory-safe") {
            let mm := mulmod(a, b, not(0))
            lo := mul(a, b)
            hi := sub(sub(mm, lo), lt(mm, lo))
        }
    }
}
