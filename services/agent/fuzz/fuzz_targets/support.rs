use std::borrow::Cow;

use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};

pub const MAX_FUZZ_INPUT: usize = 1_048_576;

pub fn input_bytes(data: &[u8]) -> Cow<'_, [u8]> {
    let Some(hex) = data.strip_prefix(b"hex:") else {
        return Cow::Borrowed(data);
    };
    let hex = hex.strip_suffix(b"\n").unwrap_or(hex);
    if hex.len() > MAX_FUZZ_INPUT.saturating_mul(2) || !hex.len().is_multiple_of(2) {
        return Cow::Borrowed(data);
    }
    let decoded: Option<Vec<u8>> = hex
        .chunks_exact(2)
        .map(|pair| Some((nibble(pair[0])? << 4) | nibble(pair[1])?))
        .collect();
    decoded.map_or(Cow::Borrowed(data), Cow::Owned)
}

fn nibble(value: u8) -> Option<u8> {
    match value {
        b'0'..=b'9' => Some(value - b'0'),
        b'a'..=b'f' => Some(value - b'a' + 10),
        b'A'..=b'F' => Some(value - b'A' + 10),
        _ => None,
    }
}

#[allow(dead_code)]
pub fn registry() -> Option<ModuleRegistry> {
    let module_maximums = [
        (ModuleId::Asset, 8),
        (ModuleId::Escrow, 7),
        (ModuleId::Budget, 7),
        (ModuleId::Stream, 7),
        (ModuleId::Service, 13),
        (ModuleId::Perps, 11),
        (ModuleId::Governance, 1),
        (ModuleId::Bridge, 1),
    ];
    let registrations: Option<Vec<_>> = module_maximums
        .into_iter()
        .map(|(module, maximum)| {
            let activity_types: Option<Vec<_>> = (1..=maximum)
                .map(|ordinal| ActivityType::new(module, ordinal).ok())
                .collect();
            ModuleRegistration::new(module, &activity_types?).ok()
        })
        .collect();
    ModuleRegistry::new(&registrations?).ok()
}
