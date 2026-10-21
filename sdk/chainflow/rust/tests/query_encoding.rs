//! Array query parameters declared `style: form, explode: false` travel as one
//! comma-joined value, not one pair per element.

use std::sync::{Arc, Mutex};

use chainflow_data_gateway::transport::{BoxFuture, SdkBody, Transport, TransportError};
use chainflow_data_gateway::*;

#[derive(Debug, Clone, Default)]
struct RecordingTransport {
    uris: Arc<Mutex<Vec<::http::Uri>>>,
}

impl Transport for RecordingTransport {
    fn execute(
        &self,
        request: ::http::Request<SdkBody>,
    ) -> BoxFuture<'_, Result<::http::Response<SdkBody>, TransportError>> {
        self.uris.lock().unwrap().push(request.uri().clone());
        Box::pin(async {
            Ok(::http::Response::builder()
                .status(200)
                .header("content-type", "application/json")
                .body(SdkBody::from_bytes("[]"))
                .unwrap())
        })
    }
}

impl RecordingTransport {
    fn only_query_pairs(&self) -> Vec<(String, String)> {
        let uris = self.uris.lock().unwrap();
        assert_eq!(uris.len(), 1, "expected exactly one request, got {uris:?}");
        let url = url::Url::parse(&uris[0].to_string()).expect("absolute request uri");
        url.query_pairs()
            .map(|(k, v)| (k.into_owned(), v.into_owned()))
            .collect()
    }

    fn raw_query(&self) -> String {
        self.uris.lock().unwrap()[0].query().unwrap_or_default().to_string()
    }
}

fn client(transport: &RecordingTransport) -> ChainflowDataGatewayClient {
    ChainflowDataGatewayClient::builder()
        .base_url("http://query.invalid")
        .transport(transport.clone())
        .max_retries(0)
        .build()
        .expect("client builds")
}

fn values_for<'a>(pairs: &'a [(String, String)], key: &str) -> Vec<&'a str> {
    pairs
        .iter()
        .filter(|(k, _)| k == key)
        .map(|(_, v)| v.as_str())
        .collect()
}

#[tokio::test]
async fn coin_gecko_list_markets_comma_joins_ids() {
    let transport = RecordingTransport::default();
    let _ = client(&transport)
        .coin_gecko()
        .list_markets()
        .ids(vec![CoinId::Bitcoin, CoinId::Ethereum])
        .send()
        .await;
    let pairs = transport.only_query_pairs();
    assert_eq!(values_for(&pairs, "ids"), vec!["bitcoin,ethereum"]);
    assert_eq!(transport.raw_query(), "ids=bitcoin%2Cethereum");
}

#[tokio::test]
async fn coin_gecko_list_markets_comma_joins_symbols() {
    let transport = RecordingTransport::default();
    let _ = client(&transport)
        .coin_gecko()
        .list_markets()
        .symbols(vec![Market::Btc, Market::Eth])
        .send()
        .await;
    let pairs = transport.only_query_pairs();
    assert_eq!(values_for(&pairs, "symbols"), vec!["btc,eth"]);
}

#[tokio::test]
async fn coin_gecko_list_simple_prices_comma_joins_ids() {
    let transport = RecordingTransport::default();
    let _ = client(&transport)
        .coin_gecko()
        .list_simple_prices()
        .ids(vec![CoinId::Bitcoin, CoinId::Ethereum])
        .send()
        .await;
    let pairs = transport.only_query_pairs();
    assert_eq!(values_for(&pairs, "ids"), vec!["bitcoin,ethereum"]);
}

#[tokio::test]
async fn coin_market_cap_list_quotes_comma_joins_symbol() {
    let transport = RecordingTransport::default();
    let _ = client(&transport)
        .coin_market_cap()
        .list_quotes()
        .symbol(vec![Symbol::Btc, Symbol::Eth])
        .send()
        .await;
    let pairs = transport.only_query_pairs();
    assert_eq!(values_for(&pairs, "symbol"), vec!["BTC,ETH"]);
}

#[tokio::test]
async fn trading_view_list_quotes_comma_joins_symbols() {
    let transport = RecordingTransport::default();
    let _ = client(&transport)
        .trading_view()
        .list_quotes(Market::Btc)
        .symbols(vec![Symbol::Btc, Symbol::Eth])
        .send()
        .await;
    let pairs = transport.only_query_pairs();
    assert_eq!(values_for(&pairs, "symbols"), vec!["BTC,ETH"]);
}
