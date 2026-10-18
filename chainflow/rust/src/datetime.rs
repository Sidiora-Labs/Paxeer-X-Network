//! Lenient deserialization for `format: date-time` values.
//!
//! chrono's stock `Deserialize` accepts only full RFC 3339, which rejects a
//! large share of real API output: .NET and Postgres-backed services routinely
//! emit `2025-12-01T09:50:03.559` or `2025-08-25T20:00:00` with no offset at
//! all, and one such field fails the whole response body.
//!
//! Reads try RFC 3339 first, then the narrow-offset spellings RFC 3339 rejects,
//! then the offset-less ones — assuming UTC only when the input carries no
//! offset of its own. Serialization is deliberately untouched: chrono's own
//! `Serialize` already round-trips a `-04:00` back as `-04:00` and re-renders
//! `Z` as `Z`, which is exactly what the server sent, so these helpers only ever
//! supply `deserialize_with`.

use chrono::TimeZone as _;
use serde::Deserialize as _;

/// The date-time type this crate's models use, per the emitter's `date_time_type`.
type Target = chrono::DateTime<chrono::FixedOffset>;

/// Offset-bearing formats accepted after RFC 3339 fails, in order.
///
/// RFC 3339 requires a full `+HH:MM` offset, so `parse_from_rfc3339` rejects the
/// `+00` and `+0000` spellings of the very same instant — and `+00` is what
/// `psql` prints for a `timestamptz` by default. `%#z` accepts all three widths.
///
/// These sit above the offset-less formats on purpose: `%#z` still requires an
/// offset to be present, so an offset-less input falls past them to the
/// UTC-assumption path rather than being silently matched here.
const OFFSET_FORMATS: &[&str] = &["%Y-%m-%dT%H:%M:%S%.f%#z", "%Y-%m-%d %H:%M:%S%.f%#z"];

/// Offset-less formats accepted after every offset-bearing attempt fails, in order.
///
/// `%.f` matches an optional fractional-second part, so one entry covers both
/// `…:03.559` and `…:03`. The space-separated spelling is what Postgres and
/// several ORMs write when the column carries no zone.
const NAIVE_FORMATS: &[&str] = &["%Y-%m-%dT%H:%M:%S%.f", "%Y-%m-%d %H:%M:%S%.f"];

/// Parses one timestamp, falling back through the offset-bearing formats and
/// then the offset-less ones.
///
/// An offset-less input is read as UTC — the only defensible reading when the
/// server declined to say, and the one that keeps the value stable across the
/// caller's local timezone. An input that does carry an offset never reaches
/// that path, so nothing is ever guessed over a stated zone.
fn parse(value: &str) -> Result<Target, String> {
    if let Ok(parsed) = chrono::DateTime::parse_from_rfc3339(value) {
        return Ok(convert(parsed));
    }
    for format in OFFSET_FORMATS {
        if let Ok(parsed) = chrono::DateTime::parse_from_str(value, format) {
            return Ok(convert(parsed));
        }
    }
    for format in NAIVE_FORMATS {
        if let Ok(naive) = chrono::NaiveDateTime::parse_from_str(value, format) {
            return Ok(convert(chrono::Utc.from_utc_datetime(&naive).fixed_offset()));
        }
    }
    Err(format!(
        "expected an RFC 3339 date-time (or `YYYY-MM-DDTHH:MM:SS[.fff][±HH[:MM]]`), got {value:?}"
    ))
}

/// Narrows a parsed fixed-offset timestamp to the crate's configured target type.
fn convert(value: chrono::DateTime<chrono::FixedOffset>) -> Target {
    value
}

/// Deserialization shim: the one place the lenient parse is wired into serde.
///
/// Every public entry point below routes through it so a container (`Option`,
/// `Vec`, `HashMap`) can reuse serde's own impl for the container itself and
/// override only the element.
struct Flexible(Target);

impl<'de> serde::Deserialize<'de> for Flexible {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        deserializer.deserialize_str(FlexibleVisitor)
    }
}

/// Parses straight out of the deserializer's own buffer.
///
/// Deserializing a `String` first would heap-allocate once per timestamp —
/// including on the common RFC 3339 path, where nothing lenient is needed — and
/// a paginated page of date-time fields pays that per row. A visitor sees the
/// borrowed `&str` instead, which is what chrono's own stock impl does. A
/// `Cow<'de, str>` would not have helped: serde's blanket impl for `Cow` always
/// yields `Cow::Owned`.
struct FlexibleVisitor;

impl serde::de::Visitor<'_> for FlexibleVisitor {
    type Value = Flexible;

    fn expecting(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("an RFC 3339 date-time string")
    }

    fn visit_str<E: serde::de::Error>(self, value: &str) -> Result<Self::Value, E> {
        parse(value).map(Flexible).map_err(serde::de::Error::custom)
    }
}

/// `#[serde(deserialize_with = "crate::datetime::deserialize")]` for a bare field.
pub fn deserialize<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<Target, D::Error> {
    Flexible::deserialize(deserializer).map(|value| value.0)
}

/// The `Option<_>` field shape. Absent and `null` both stay `None`.
pub mod option {
    use serde::Deserialize as _;

    /// `#[serde(deserialize_with = "crate::datetime::option::deserialize")]`.
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<Option<super::Target>, D::Error> {
        Ok(Option::<super::Flexible>::deserialize(deserializer)?.map(|value| value.0))
    }
}

/// The `Vec<_>` field shape.
pub mod vec {
    use serde::Deserialize as _;

    /// `#[serde(deserialize_with = "crate::datetime::vec::deserialize")]`.
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<Vec<super::Target>, D::Error> {
        Ok(Vec::<super::Flexible>::deserialize(deserializer)?
            .into_iter()
            .map(|value| value.0)
            .collect())
    }
}

/// The `Option<Vec<_>>` field shape.
pub mod option_vec {
    use serde::Deserialize as _;

    /// `#[serde(deserialize_with = "crate::datetime::option_vec::deserialize")]`.
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(
        deserializer: D,
    ) -> Result<Option<Vec<super::Target>>, D::Error> {
        Ok(Option::<Vec<super::Flexible>>::deserialize(deserializer)?
            .map(|values| values.into_iter().map(|value| value.0).collect()))
    }
}

/// The `HashMap<String, _>` field shape.
pub mod map {
    use serde::Deserialize as _;

    /// `#[serde(deserialize_with = "crate::datetime::map::deserialize")]`.
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(
        deserializer: D,
    ) -> Result<std::collections::HashMap<String, super::Target>, D::Error> {
        Ok(
            std::collections::HashMap::<String, super::Flexible>::deserialize(deserializer)?
                .into_iter()
                .map(|(key, value)| (key, value.0))
                .collect(),
        )
    }
}

/// The `Option<HashMap<String, _>>` field shape.
pub mod option_map {
    use serde::Deserialize as _;

    /// `#[serde(deserialize_with = "crate::datetime::option_map::deserialize")]`.
    pub fn deserialize<'de, D: serde::Deserializer<'de>>(
        deserializer: D,
    ) -> Result<Option<std::collections::HashMap<String, super::Target>>, D::Error> {
        Ok(
            Option::<std::collections::HashMap<String, super::Flexible>>::deserialize(deserializer)?
                .map(|values| values.into_iter().map(|(key, value)| (key, value.0)).collect()),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Debug, serde::Serialize, serde::Deserialize)]
    struct Wrapper {
        #[serde(deserialize_with = "crate::datetime::deserialize")]
        at: Target,
    }

    #[derive(Debug, serde::Serialize, serde::Deserialize)]
    struct OptionalWrapper {
        #[serde(default, deserialize_with = "crate::datetime::option::deserialize")]
        at: Option<Target>,
    }

    fn decode(raw: &str) -> Target {
        serde_json::from_str::<Wrapper>(&format!(r#"{{"at":"{raw}"}}"#))
            .expect("decodes")
            .at
    }

    #[test]
    fn accepts_rfc_3339_with_and_without_fractional_seconds() {
        assert_eq!(decode("2025-12-01T09:50:03.559Z").timestamp(), 1764582603);
        assert_eq!(decode("2025-12-01T09:50:03Z").timestamp(), 1764582603);
    }

    #[test]
    fn accepts_offset_less_timestamps_as_utc() {
        // The .NET/Postgres shape that fails the whole response body against
        // chrono's stock impl.
        assert_eq!(decode("2025-12-01T09:50:03.559").timestamp(), 1764582603);
        assert_eq!(decode("2025-08-25T20:00:00").timestamp(), 1756152000);
        assert_eq!(decode("2025-08-25 20:00:00").timestamp(), 1756152000);
    }

    #[test]
    fn accepts_the_narrow_offsets_rfc_3339_rejects() {
        // `2025-12-01 09:50:03+00` is `psql`'s default `timestamptz` rendering;
        // RFC 3339 wants `+00:00` and rejects both narrower spellings outright.
        assert_eq!(decode("2025-12-01 09:50:03+00").timestamp(), 1764582603);
        assert_eq!(decode("2025-12-01 09:50:03.559+00").timestamp(), 1764582603);
        assert_eq!(decode("2025-12-01T09:50:03+0000").timestamp(), 1764582603);
        // A stated non-UTC offset is honored, never re-read as UTC.
        assert_eq!(decode("2025-12-01T09:50:03-0400").timestamp(), 1764597003);
    }

    #[test]
    fn reports_the_offending_value_when_nothing_parses() {
        let error = serde_json::from_str::<Wrapper>(r#"{"at":"not-a-date"}"#)
            .expect_err("rejects")
            .to_string();
        assert!(error.contains("not-a-date"), "{error}");
    }

    #[test]
    fn optional_fields_treat_absent_and_null_alike() {
        assert!(
            serde_json::from_str::<OptionalWrapper>("{}")
                .expect("decodes")
                .at
                .is_none()
        );
        assert!(
            serde_json::from_str::<OptionalWrapper>(r#"{"at":null}"#)
                .expect("decodes")
                .at
                .is_none()
        );
    }

    #[test]
    fn serialization_is_left_to_chrono() {
        // The offset the server sent has to survive the round trip; rewriting
        // every `-04:00` to `+00:00` loses information for no benefit.
        let wrapper: Wrapper = serde_json::from_str(r#"{"at":"2024-11-16T08:15:33.4364067-04:00"}"#).expect("decodes");
        let encoded = serde_json::to_string(&wrapper).expect("encodes");
        assert!(encoded.contains("-04:00"), "{encoded}");
    }
}
