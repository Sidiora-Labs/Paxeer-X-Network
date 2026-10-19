pub fn hex_encode(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
        encoded.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    encoded
}

pub fn hex_decode(name: &str, encoded: &str) -> Result<Vec<u8>, String> {
    if !encoded.len().is_multiple_of(2) {
        return Err(format!(
            "{name} must contain an even number of hexadecimal characters"
        ));
    }
    encoded
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let pair =
                std::str::from_utf8(pair).map_err(|_| format!("{name} contains invalid UTF-8"))?;
            u8::from_str_radix(pair, 16)
                .map_err(|_| format!("{name} contains non-hexadecimal input"))
        })
        .collect()
}

pub fn fixed_hex<const N: usize>(name: &str, encoded: &str) -> Result<[u8; N], String> {
    let decoded = hex_decode(name, encoded)?;
    decoded
        .try_into()
        .map_err(|bytes: Vec<u8>| format!("{name} must be {N} bytes, got {}", bytes.len()))
}
