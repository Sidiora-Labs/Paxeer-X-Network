/// Market identifier — u16 supports up to 65 535 markets.
pub type MarketId = u16;

/// Total number of markets at genesis.
pub const MARKET_COUNT: usize = 18;

/// Static metadata for a market.
#[derive(Debug, Clone, Copy)]
pub struct MarketMeta {
    pub id: MarketId,
    pub symbol: &'static str,
    pub name: &'static str,
}

/// All 18 markets, indexed by `MarketId`.
pub const MARKETS: [MarketMeta; MARKET_COUNT] = [
    MarketMeta { id: 0,  symbol: "BTC",    name: "Bitcoin" },
    MarketMeta { id: 1,  symbol: "ETH",    name: "Ethereum" },
    MarketMeta { id: 2,  symbol: "SOL",    name: "Solana" },
    MarketMeta { id: 3,  symbol: "AVAX",   name: "Avalanche" },
    MarketMeta { id: 4,  symbol: "LINK",   name: "Chainlink" },
    MarketMeta { id: 5,  symbol: "TSLA",   name: "Tesla" },
    MarketMeta { id: 6,  symbol: "NVDA",   name: "NVIDIA" },
    MarketMeta { id: 7,  symbol: "NAS100", name: "Nasdaq 100" },
    MarketMeta { id: 8,  symbol: "XAU",    name: "Gold" },
    MarketMeta { id: 9,  symbol: "SPX500", name: "S&P 500" },
    MarketMeta { id: 10, symbol: "GOOGL",  name: "Alphabet" },
    MarketMeta { id: 11, symbol: "PAX",    name: "Paxeer" },
    MarketMeta { id: 12, symbol: "SID",    name: "Sidiora" },
    MarketMeta { id: 13, symbol: "HYPE",   name: "Hyperliquid" },
    MarketMeta { id: 14, symbol: "XRP",    name: "Ripple" },
    MarketMeta { id: 15, symbol: "ASTER",  name: "Aster" },
    MarketMeta { id: 16, symbol: "TRUMP",  name: "Trump" },
    MarketMeta { id: 17, symbol: "BNB",    name: "BNB" },
];

/// Look up a market by ID. Returns `None` if out of range.
pub const fn market_by_id(id: MarketId) -> Option<&'static MarketMeta> {
    if (id as usize) < MARKET_COUNT {
        Some(&MARKETS[id as usize])
    } else {
        None
    }
}

/// Look up a market by symbol (case-sensitive).
pub fn market_by_symbol(symbol: &str) -> Option<&'static MarketMeta> {
    MARKETS.iter().find(|m| m.symbol == symbol)
}

/// Validate that a market ID is within the known set.
pub const fn is_valid_market(id: MarketId) -> bool {
    (id as usize) < MARKET_COUNT
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn all_markets_indexed() {
        for (i, m) in MARKETS.iter().enumerate() {
            assert_eq!(m.id as usize, i);
        }
    }

    #[test]
    fn lookup_by_id() {
        let btc = market_by_id(0).unwrap();
        assert_eq!(btc.symbol, "BTC");

        let bnb = market_by_id(17).unwrap();
        assert_eq!(bnb.symbol, "BNB");

        assert!(market_by_id(18).is_none());
    }

    #[test]
    fn lookup_by_symbol() {
        let sol = market_by_symbol("SOL").unwrap();
        assert_eq!(sol.id, 2);
        assert!(market_by_symbol("DOGE").is_none());
    }
}
