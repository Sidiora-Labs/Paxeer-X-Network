# Changelog

All notable changes to the Paxport Wallet will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Sentry error tracking and session replay
- Offline detection with NetworkErrorScreen
- Quote expiry countdown and auto-refresh in swap quotes
- Transaction error modals (gas, revert, timeout, ratelimit)
- Rate limiting on API proxy routes
- LocalStorage caching for token metadata with TTL
- Server-side proxy for Sidiora metadata and logo APIs
- Crossverse price and candle data queries

### Changed
- Fixed React hook usage errors in PortfolioWidget
- Improved swap modal styling and error handling
- Enhanced portfolio data fetching and filtering

### Fixed
- React error #310 on portfolio page load
- ORB errors on token logos by proxying through same-origin route
- Disabled query loading state causing perpetual spinner

### Security
- Added rate limiting middleware to all API proxy routes
- Improved input validation and sanitization

## [1.0.0] - 2024-XX-XX

### Added
- Initial release of Paxport Wallet
- Multi-chain support
- Swap functionality
- Portfolio tracking
- Token discovery
- Send/receive transactions
- Onboarding flow
- Embedded wallet support

[Unreleased]: https://github.com/paxeer/Paxport-v2/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/paxeer/Paxport-v2/releases/tag/v1.0.0
