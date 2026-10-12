use http::{HeaderName, HeaderValue, Method, StatusCode, header};

use crate::util::compare_lowercase_ascii;

#[cfg(feature = "server")]
pub(crate) trait StatusCodeExt {
    /// Check if the status code requires a body according to HTTP spec.
    ///
    /// According to the HTTP specification, the following status codes must not include a message body:
    /// - 1xx (Informational): 100, 101, etc.
    /// - 204 (No Content)
    /// - 304 (Not Modified)
    ///
    /// All other status codes can include a message body.
    fn body_allowed(&self) -> bool;
}

#[cfg(feature = "server")]
impl StatusCodeExt for StatusCode {
    fn body_allowed(&self) -> bool {
        !self.is_informational()
            && *self != StatusCode::NO_CONTENT
            && *self != StatusCode::NOT_MODIFIED
    }
}

pub(crate) trait MethodExt {
    #[cfg(feature = "client")]
    fn is_http10(&self) -> bool;
    #[cfg(feature = "client")]
    fn is_http11(&self) -> bool;
    fn need_request_body(&self) -> bool;
    fn allow_request_body(&self) -> bool;
    #[cfg(feature = "client")]
    fn verify_version(&self, version: http::Version) -> Result<(), crate::Error>;
}

impl MethodExt for Method {
    #[cfg(feature = "client")]
    fn is_http10(&self) -> bool {
        self == Method::GET || self == Method::HEAD || self == Method::POST
    }

    #[cfg(feature = "client")]
    fn is_http11(&self) -> bool {
        self == Method::PUT
            || self == Method::DELETE
            || self == Method::CONNECT
            || self == Method::OPTIONS
            || self == Method::TRACE
            || self == Method::PATCH
    }

    fn need_request_body(&self) -> bool {
        self == Method::POST || self == Method::PUT || self == Method::PATCH
    }

    fn allow_request_body(&self) -> bool {
        self != Method::HEAD && self != Method::CONNECT
    }

    #[cfg(feature = "client")]
    fn verify_version(&self, v: http::Version) -> Result<(), crate::Error> {
        use crate::Error;
        use http::Version;
        if v != Version::HTTP_10 && v != Version::HTTP_11 {
            return Err(Error::UnsupportedVersion);
        }

        let method_ok = self.is_http10() || v == Version::HTTP_11 && self.is_http11();

        if !method_ok {
            return Err(Error::MethodVersionMismatch(self.clone(), v));
        }

        Ok(())
    }
}

pub(crate) trait HeaderIterExt {
    /// Whether any header line named `key` lists `token` as one of its
    /// comma-separated elements.
    ///
    /// Elements are trimmed of surrounding whitespace and compared ASCII
    /// case-insensitively, as required for list-based fields such as
    /// `Connection` (RFC 9110 §7.6.1) and `Expect` (RFC 9110 §10.1.1).
    /// `token` must be given in lowercase.
    fn has(self, key: HeaderName, token: &str) -> bool;
    fn has_expect_100(self) -> bool;
}

impl<'a, I: Iterator<Item = (&'a HeaderName, &'a HeaderValue)>> HeaderIterExt for I {
    fn has(self, key: HeaderName, token: &str) -> bool {
        self.filter(|(name, _)| **name == key)
            .filter_map(|(_, value)| value.to_str().ok())
            .flat_map(|value| value.split(','))
            .any(|element| compare_lowercase_ascii(element.trim(), token))
    }

    fn has_expect_100(self) -> bool {
        self.has(header::EXPECT, "100-continue")
    }
}

#[cfg(feature = "client")]
pub(crate) trait StatusExt {
    /// Detect 307/308 redirect
    fn is_redirect_retaining_status(&self) -> bool;
}

#[cfg(feature = "client")]
impl StatusExt for StatusCode {
    fn is_redirect_retaining_status(&self) -> bool {
        *self == StatusCode::TEMPORARY_REDIRECT || *self == StatusCode::PERMANENT_REDIRECT
    }
}

#[cfg(feature = "client")]
pub trait SchemeExt {
    fn default_port(&self) -> Option<u16>;
}

#[cfg(feature = "client")]
impl SchemeExt for http::uri::Scheme {
    fn default_port(&self) -> Option<u16> {
        use http::uri::Scheme;
        if *self == Scheme::HTTPS {
            Some(443)
        } else if *self == Scheme::HTTP {
            Some(80)
        } else {
            debug!("Unknown scheme: {}", self);
            None
        }
    }
}

#[cfg(feature = "client")]
pub(crate) trait AuthorityExt {
    fn userinfo(&self) -> Option<&str>;
    fn username(&self) -> Option<&str>;
    fn password(&self) -> Option<&str>;
}

// NB: Treating &str with direct indexes is OK, since Uri parsed the Authority,
// and ensured it's all ASCII (or %-encoded).
#[cfg(feature = "client")]
impl AuthorityExt for http::uri::Authority {
    fn userinfo(&self) -> Option<&str> {
        let s = self.as_str();
        s.rfind('@').map(|i| &s[..i])
    }

    fn username(&self) -> Option<&str> {
        self.userinfo()
            .map(|a| a.rfind(':').map(|i| &a[..i]).unwrap_or(a))
    }

    fn password(&self) -> Option<&str> {
        self.userinfo()
            .and_then(|a| a.rfind(':').map(|i| &a[i + 1..]))
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use http::HeaderMap;

    fn headers(lines: &[(&'static str, &'static str)]) -> HeaderMap {
        let mut map = HeaderMap::new();
        for (name, value) in lines {
            map.append(*name, HeaderValue::from_static(value));
        }
        map
    }

    // Connection options are comma-separated tokens compared case-insensitively.
    // https://www.rfc-editor.org/rfc/rfc9110#section-7.6.1

    #[test]
    fn has_is_case_insensitive() {
        let h = headers(&[("connection", "Close")]);
        assert!(h.iter().has(header::CONNECTION, "close"));

        let h = headers(&[("connection", "Keep-Alive")]);
        assert!(h.iter().has(header::CONNECTION, "keep-alive"));
    }

    #[test]
    fn has_finds_element_in_list() {
        let h = headers(&[("connection", "keep-alive, close")]);
        assert!(h.iter().has(header::CONNECTION, "close"));
        assert!(h.iter().has(header::CONNECTION, "keep-alive"));

        let h = headers(&[("connection", "close,Upgrade")]);
        assert!(h.iter().has(header::CONNECTION, "close"));
        assert!(h.iter().has(header::CONNECTION, "upgrade"));
    }

    #[test]
    fn has_across_repeated_lines() {
        let h = headers(&[("connection", "keep-alive"), ("connection", "close")]);
        assert!(h.iter().has(header::CONNECTION, "close"));
    }

    #[test]
    fn has_no_partial_or_wrong_header_match() {
        let h = headers(&[("connection", "closed")]);
        assert!(!h.iter().has(header::CONNECTION, "close"));

        let h = headers(&[("connection", "keep-alive")]);
        assert!(!h.iter().has(header::CONNECTION, "close"));

        let h = headers(&[("upgrade", "close")]);
        assert!(!h.iter().has(header::CONNECTION, "close"));

        let h = headers(&[("connection", "")]);
        assert!(!h.iter().has(header::CONNECTION, "close"));
    }

    #[test]
    fn has_expect_100_is_case_insensitive() {
        // https://www.rfc-editor.org/rfc/rfc9110#section-10.1.1
        let h = headers(&[("expect", "100-Continue")]);
        assert!(h.iter().has_expect_100());

        let h = headers(&[("expect", "100-continue")]);
        assert!(h.iter().has_expect_100());
    }
}
