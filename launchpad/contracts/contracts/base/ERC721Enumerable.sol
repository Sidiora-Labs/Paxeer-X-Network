// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./ERC721Base.sol";

/// @title ERC721Enumerable
/// @notice Enumeration extension for ERC721: totalSupply, tokenByIndex, tokenOfOwnerByIndex
abstract contract ERC721Enumerable is ERC721Base {
    uint256[] private _allTokens;
    mapping(uint256 => uint256) private _allTokensIndex;
    mapping(address => uint256[]) private _ownedTokens;
    mapping(uint256 => uint256) private _ownedTokensIndex;

    error IndexOutOfBounds();

    /// @notice ERC165: add ERC721Enumerable interface support
    function supportsInterface(bytes4 interfaceId) public view virtual override returns (bool) {
        return
            interfaceId == 0x780e9d63 || // ERC721Enumerable
            super.supportsInterface(interfaceId);
    }

    /// @notice Returns the total number of tokens
    function totalSupply() public view returns (uint256) {
        return _allTokens.length;
    }

    /// @notice Returns the token ID at a given index across all tokens
    function tokenByIndex(uint256 index) external view returns (uint256) {
        if (index >= _allTokens.length) revert IndexOutOfBounds();
        return _allTokens[index];
    }

    /// @notice Returns the token ID at a given index for an owner
    function tokenOfOwnerByIndex(address owner_, uint256 index) external view returns (uint256) {
        if (index >= _ownedTokens[owner_].length) revert IndexOutOfBounds();
        return _ownedTokens[owner_][index];
    }

    function _mint(address to, uint256 tokenId) internal virtual override {
        super._mint(to, tokenId);
        _addTokenToAllEnumeration(tokenId);
        _addTokenToOwnerEnumeration(to, tokenId);
    }

    function _burn(uint256 tokenId) internal virtual override {
        address owner_ = ownerOf(tokenId);
        _removeTokenFromOwnerEnumeration(owner_, tokenId);
        _removeTokenFromAllEnumeration(tokenId);
        super._burn(tokenId);
    }

    function _transfer(address from, address to, uint256 tokenId) internal virtual override {
        if (from != to) {
            _removeTokenFromOwnerEnumeration(from, tokenId);
            _addTokenToOwnerEnumeration(to, tokenId);
        }
        super._transfer(from, to, tokenId);
    }

    function _addTokenToAllEnumeration(uint256 tokenId) private {
        _allTokensIndex[tokenId] = _allTokens.length;
        _allTokens.push(tokenId);
    }

    function _addTokenToOwnerEnumeration(address to, uint256 tokenId) private {
        _ownedTokensIndex[tokenId] = _ownedTokens[to].length;
        _ownedTokens[to].push(tokenId);
    }

    function _removeTokenFromAllEnumeration(uint256 tokenId) private {
        uint256 lastIndex = _allTokens.length - 1;
        uint256 tokenIndex = _allTokensIndex[tokenId];

        uint256 lastTokenId = _allTokens[lastIndex];
        _allTokens[tokenIndex] = lastTokenId;
        _allTokensIndex[lastTokenId] = tokenIndex;

        _allTokens.pop();
        delete _allTokensIndex[tokenId];
    }

    function _removeTokenFromOwnerEnumeration(address from, uint256 tokenId) private {
        uint256 lastIndex = _ownedTokens[from].length - 1;
        uint256 tokenIndex = _ownedTokensIndex[tokenId];

        if (tokenIndex != lastIndex) {
            uint256 lastTokenId = _ownedTokens[from][lastIndex];
            _ownedTokens[from][tokenIndex] = lastTokenId;
            _ownedTokensIndex[lastTokenId] = tokenIndex;
        }

        _ownedTokens[from].pop();
        delete _ownedTokensIndex[tokenId];
    }
}
