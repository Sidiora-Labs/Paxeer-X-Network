use serde_json::{json, Value};

use crate::http::{validate_idempotency_key, Client};

pub fn test_payment(
    client: &Client,
    source: &str,
    destination: &str,
    currency: &str,
    amount: &str,
    idempotency_key: &str,
) -> Result<Value, String> {
    validate_amount(amount)?;
    validate_idempotency_key(idempotency_key)?;
    let quote_request = json!({
        "source": source,
        "destination": destination,
        "money": {
            "currency": currency,
            "amount": amount,
        }
    });
    let quote = client.post("/v1/moves/quote", &quote_request, None)?;
    let quote_id = quote
        .pointer("/result/quote_id")
        .and_then(Value::as_str)
        .ok_or_else(|| "move quote response omitted quote_id".to_string())?;
    let committed = client.post(
        "/v1/moves",
        &json!({"quote_id": quote_id}),
        Some(idempotency_key),
    )?;
    Ok(json!({
        "quote": quote,
        "journey": committed,
        "idempotency_key": idempotency_key,
    }))
}

fn validate_amount(value: &str) -> Result<(), String> {
    let amount = value
        .parse::<u128>()
        .map_err(|_| "amount must be an unsigned protocol integer".to_string())?;
    if amount == 0 {
        return Err("amount must be greater than zero".into());
    }
    Ok(())
}
