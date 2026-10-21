//! Integer-preserving serialization for `type: number` schemas.
//!
//! OpenAPI's `number` maps to `f64`, and serde_json renders `100.0f64` as
//! `100.0`. That is a valid JSON number and numerically identical to `100`, but
//! it is not what the spec's own examples show, not what the Go and TypeScript
//! SDKs send, and some APIs reject the decimal form for an integer-valued field.
//!
//! These helpers render an integral value as a JSON integer and leave everything
//! else on serde_json's own float rendering. They are wired in per field via
//! `#[serde(serialize_with = …)]` — never as a global formatter — so only values
//! the document actually typed `number` are reshaped. A caller's free-form
//! `serde_json::Value` payload and an untagged union's float arm both keep their
//! exact rendering.
//!
//! Deserialization is untouched: `f64`'s own impl already accepts both `100` and
//! `100.0`.

/// Largest magnitude an `f64` represents as an exact integer (2^53).
///
/// Past this, consecutive integers are no longer all representable, so `as i64`
/// would print a value the `f64` does not actually hold.
const EXACT_INTEGER_LIMIT: f64 = 9_007_199_254_740_992.0;

/// Renders an integral value as a JSON integer, everything else as a float.
///
/// A non-finite value falls through to serde_json, which renders it as `null` —
/// the same thing it did before this module existed.
fn write<S: serde::Serializer>(value: f64, serializer: S) -> Result<S::Ok, S::Error> {
    if value.is_finite() && value.fract() == 0.0 && value.abs() <= EXACT_INTEGER_LIMIT {
        // `-0.0` normalizes to `0`; JSON has no signed zero to preserve.
        return serializer.serialize_i64(value as i64);
    }
    serializer.serialize_f64(value)
}

/// One value, so the container helpers below can lean on serde's own `Option`,
/// sequence and map serialization instead of reimplementing each.
struct Integral(f64);

impl serde::Serialize for Integral {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        write(self.0, serializer)
    }
}

/// A slice of values, each rendered through `Integral`.
struct Seq<'a>(&'a [f64]);

impl serde::Serialize for Seq<'_> {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.collect_seq(self.0.iter().copied().map(Integral))
    }
}

/// A map of values, each rendered through `Integral`.
struct Map<'a>(&'a std::collections::HashMap<String, f64>);

impl serde::Serialize for Map<'_> {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.collect_map(self.0.iter().map(|(key, value)| (key, Integral(*value))))
    }
}

/// `#[serde(serialize_with = "crate::number::serialize")]` for a bare field.
pub fn serialize<S: serde::Serializer>(value: &f64, serializer: S) -> Result<S::Ok, S::Error> {
    write(*value, serializer)
}

/// The `Option<_>` field shape.
pub mod option {
    use serde::Serialize as _;

    /// `#[serde(serialize_with = "crate::number::option::serialize")]`.
    pub fn serialize<S: serde::Serializer>(value: &Option<f64>, serializer: S) -> Result<S::Ok, S::Error> {
        value.map(super::Integral).serialize(serializer)
    }
}

/// The `Vec<_>` field shape.
pub mod vec {
    use serde::Serialize as _;

    /// `#[serde(serialize_with = "crate::number::vec::serialize")]`.
    ///
    /// Takes `&[f64]`, not `&Vec<f64>`: `clippy::ptr_arg` is denied by the
    /// generated CI, and serde's call site deref-coerces either way.
    pub fn serialize<S: serde::Serializer>(value: &[f64], serializer: S) -> Result<S::Ok, S::Error> {
        super::Seq(value).serialize(serializer)
    }
}

/// The `Option<Vec<_>>` field shape.
pub mod option_vec {
    use serde::Serialize as _;

    /// `#[serde(serialize_with = "crate::number::option_vec::serialize")]`.
    pub fn serialize<S: serde::Serializer>(value: &Option<Vec<f64>>, serializer: S) -> Result<S::Ok, S::Error> {
        value.as_deref().map(super::Seq).serialize(serializer)
    }
}

/// The `HashMap<String, _>` field shape.
pub mod map {
    use serde::Serialize as _;

    /// `#[serde(serialize_with = "crate::number::map::serialize")]`.
    pub fn serialize<S: serde::Serializer>(
        value: &std::collections::HashMap<String, f64>,
        serializer: S,
    ) -> Result<S::Ok, S::Error> {
        super::Map(value).serialize(serializer)
    }
}

/// The `Option<HashMap<String, _>>` field shape.
pub mod option_map {
    use serde::Serialize as _;

    /// `#[serde(serialize_with = "crate::number::option_map::serialize")]`.
    pub fn serialize<S: serde::Serializer>(
        value: &Option<std::collections::HashMap<String, f64>>,
        serializer: S,
    ) -> Result<S::Ok, S::Error> {
        value.as_ref().map(super::Map).serialize(serializer)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Debug, serde::Serialize)]
    struct Wrapper {
        #[serde(serialize_with = "crate::number::serialize")]
        v: f64,
    }

    #[derive(Debug, serde::Serialize)]
    struct Containers {
        #[serde(serialize_with = "crate::number::option::serialize")]
        o: Option<f64>,
        #[serde(serialize_with = "crate::number::vec::serialize")]
        l: Vec<f64>,
        #[serde(serialize_with = "crate::number::map::serialize")]
        m: std::collections::HashMap<String, f64>,
    }

    fn encode(value: f64) -> String {
        serde_json::to_string(&Wrapper { v: value }).expect("encodes")
    }

    #[test]
    fn integral_values_render_without_a_fractional_part() {
        assert_eq!(encode(100.0), r#"{"v":100}"#);
        assert_eq!(encode(0.0), r#"{"v":0}"#);
        // JSON has no signed zero, so `-0.0` is just `0`.
        assert_eq!(encode(-0.0), r#"{"v":0}"#);
        assert_eq!(encode(-100.0), r#"{"v":-100}"#);
    }

    #[test]
    fn genuinely_fractional_values_are_untouched() {
        assert_eq!(encode(1.5), r#"{"v":1.5}"#);
        assert_eq!(encode(0.1), r#"{"v":0.1}"#);
    }

    #[test]
    fn values_past_exact_integer_range_stay_floats() {
        // `as i64` stops being lossless past 2^53, so the float rendering is the
        // only one that still describes the value the f64 actually holds. Compared
        // against serde_json's own rendering rather than a pinned literal, because
        // "unchanged" is the claim; the `e` check keeps that from passing
        // vacuously if both sides ever became integers.
        for value in [1e300, EXACT_INTEGER_LIMIT * 2.0] {
            let encoded = encode(value);
            assert_eq!(encoded, format!(r#"{{"v":{}}}"#, serde_json::json!(value)));
            assert!(encoded.contains('e'), "{encoded}");
        }
    }

    #[test]
    fn non_finite_values_keep_serde_jsons_own_rendering() {
        assert_eq!(encode(f64::NAN), r#"{"v":null}"#);
        assert_eq!(encode(f64::INFINITY), r#"{"v":null}"#);
    }

    #[test]
    fn containers_reshape_every_element() {
        let mut m = std::collections::HashMap::new();
        m.insert("fee".to_string(), 25.0);
        let encoded = serde_json::to_string(&Containers {
            o: Some(0.0),
            l: vec![100.0, 1.5],
            m,
        })
        .expect("encodes");

        assert!(encoded.contains(r#""o":0"#), "{encoded}");
        assert!(encoded.contains(r#""l":[100,1.5]"#), "{encoded}");
        assert!(encoded.contains(r#""fee":25"#), "{encoded}");
    }

    #[test]
    fn an_unset_optional_still_serializes_as_null() {
        let encoded = serde_json::to_string(&Containers {
            o: None,
            l: Vec::new(),
            m: std::collections::HashMap::new(),
        })
        .expect("encodes");

        assert!(encoded.contains(r#""o":null"#), "{encoded}");
    }

    #[test]
    fn an_integer_rendering_still_decodes_back_into_an_f64() {
        // The whole point: the reshaped payload must remain readable by the same
        // model that produced it.
        #[derive(serde::Deserialize)]
        struct Read {
            v: f64,
        }
        let decoded: Read = serde_json::from_str(&encode(100.0)).expect("decodes");
        assert_eq!(decoded.v, 100.0);
    }
}
