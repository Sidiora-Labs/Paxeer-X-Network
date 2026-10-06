use serde::Deserialize;
use std::fs;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::path::Path;
use std::time::Duration;
use zeroize::{Zeroize, Zeroizing};

const IO_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_REPLY_LINE: usize = 1024;
const MAX_MESSAGE_BYTES: u64 = 4096;

/// Where verification mails go: `smtp://` speaks plain SMTP (a local relay),
/// `smtps://` speaks SMTP inside implicit TLS. Userinfo in the URL is sent as
/// AUTH PLAIN, and only over `smtps://`.
#[derive(Debug, PartialEq, Eq)]
pub struct Smtp {
    tls: bool,
    host: String,
    port: u16,
    credentials: Option<(Zeroizing<String>, Zeroizing<String>)>,
    from: String,
}

#[derive(Deserialize)]
struct Verification {
    kind: String,
    email: String,
    token: String,
    expires_at: u64,
}

impl Smtp {
    pub fn parse(url: &str, from: &str) -> Result<Self, String> {
        let (tls, rest) = if let Some(rest) = url.strip_prefix("smtps://") {
            (true, rest)
        } else if let Some(rest) = url.strip_prefix("smtp://") {
            (false, rest)
        } else {
            return Err("LAYERX_IDENTITY_SMTP_URL must start with smtp:// or smtps://".to_owned());
        };
        let rest = rest.trim_end_matches('/');
        let (userinfo, authority) = match rest.rsplit_once('@') {
            Some((userinfo, authority)) => (Some(userinfo), authority),
            None => (None, rest),
        };
        let (host, port) = match authority.rsplit_once(':') {
            Some((host, port)) => (
                host,
                port.parse::<u16>()
                    .map_err(|_| "LAYERX_IDENTITY_SMTP_URL port is invalid".to_owned())?,
            ),
            None => (authority, if tls { 465 } else { 25 }),
        };
        if host.is_empty() || host.contains('/') {
            return Err("LAYERX_IDENTITY_SMTP_URL host is invalid".to_owned());
        }
        let credentials = match userinfo {
            None => None,
            Some(_) if !tls => {
                return Err("LAYERX_IDENTITY_SMTP_URL credentials require smtps://".to_owned())
            }
            Some(userinfo) => {
                let (user, password) = userinfo.split_once(':').unwrap_or((userinfo, ""));
                Some((percent_decode(user)?, percent_decode(password)?))
            }
        };
        if !valid_address(from) {
            return Err("LAYERX_IDENTITY_SMTP_FROM is not a mail address".to_owned());
        }
        Ok(Self {
            tls,
            host: host.to_owned(),
            port,
            credentials,
            from: from.to_owned(),
        })
    }

    /// Sends every queued verification in `outbox` and removes each one the
    /// server accepted. Returns how many were sent; the first failure stops
    /// the pass and leaves that file and the rest queued for the next pass.
    pub fn drain(&self, outbox: &Path) -> Result<usize, String> {
        let entries = match fs::read_dir(outbox) {
            Ok(entries) => entries,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(0),
            Err(error) => return Err(error.to_string()),
        };
        let mut queued: Vec<_> = entries
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .filter(|path| {
                path.extension()
                    .is_some_and(|extension| extension == "json")
            })
            .collect();
        queued.sort();
        let mut sent = 0;
        for path in queued {
            let mut raw = Zeroizing::new(Vec::new());
            fs::File::open(&path)
                .and_then(|file| file.take(MAX_MESSAGE_BYTES).read_to_end(&mut raw))
                .map_err(|error| format!("{}: {error}", path.display()))?;
            let verification: Verification = serde_json::from_slice(&raw)
                .map_err(|error| format!("{}: {error}", path.display()))?;
            if verification.kind != "signup_verification" || !valid_address(&verification.email) {
                return Err(format!("{}: not a signup verification", path.display()));
            }
            let mut verification = verification;
            let result = self.send(&verification);
            verification.token.zeroize();
            result?;
            fs::remove_file(&path).map_err(|error| format!("{}: {error}", path.display()))?;
            sent += 1;
        }
        Ok(sent)
    }

    fn send(&self, verification: &Verification) -> Result<(), String> {
        let stream = TcpStream::connect((self.host.as_str(), self.port))
            .map_err(|error| format!("SMTP connect: {error}"))?;
        stream
            .set_read_timeout(Some(IO_TIMEOUT))
            .and_then(|()| stream.set_write_timeout(Some(IO_TIMEOUT)))
            .map_err(|error| error.to_string())?;
        if self.tls {
            let connector = native_tls::TlsConnector::new().map_err(|error| error.to_string())?;
            let stream = connector
                .connect(&self.host, stream)
                .map_err(|error| format!("SMTP TLS: {error}"))?;
            self.dialogue(BufReader::new(stream), verification)
        } else {
            self.dialogue(BufReader::new(stream), verification)
        }
    }

    fn dialogue<S: Read + Write>(
        &self,
        mut session: BufReader<S>,
        verification: &Verification,
    ) -> Result<(), String> {
        expect(&mut session, 220)?;
        command(&mut session, b"EHLO layerx-identity\r\n", 250)?;
        if let Some((user, password)) = &self.credentials {
            let mut plain = Zeroizing::new(Vec::new());
            plain.push(0);
            plain.extend_from_slice(user.as_bytes());
            plain.push(0);
            plain.extend_from_slice(password.as_bytes());
            let line = Zeroizing::new(format!("AUTH PLAIN {}\r\n", base64(&plain).as_str()));
            command(&mut session, line.as_bytes(), 235)?;
        }
        command(
            &mut session,
            format!("MAIL FROM:<{}>\r\n", self.from).as_bytes(),
            250,
        )?;
        command(
            &mut session,
            format!("RCPT TO:<{}>\r\n", verification.email).as_bytes(),
            250,
        )?;
        command(&mut session, b"DATA\r\n", 354)?;
        let message = Zeroizing::new(format!(
            "From: <{from}>\r\nTo: <{to}>\r\nSubject: Verify your Paxeer X Network account\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYour verification code is {token}\r\nIt expires at unix time {expires}.\r\n.\r\n",
            from = self.from,
            to = verification.email,
            token = verification.token.as_str(),
            expires = verification.expires_at,
        ));
        command(&mut session, message.as_bytes(), 250)?;
        command(&mut session, b"QUIT\r\n", 221)
    }
}

fn command<S: Read + Write>(
    session: &mut BufReader<S>,
    line: &[u8],
    code: u16,
) -> Result<(), String> {
    session
        .get_mut()
        .write_all(line)
        .and_then(|()| session.get_mut().flush())
        .map_err(|error| format!("SMTP write: {error}"))?;
    expect(session, code)
}

fn expect<S: Read>(session: &mut BufReader<S>, code: u16) -> Result<(), String> {
    loop {
        let mut line = String::new();
        (&mut *session)
            .take(MAX_REPLY_LINE as u64)
            .read_line(&mut line)
            .map_err(|error| format!("SMTP read: {error}"))?;
        let status = line.get(..3).and_then(|status| status.parse::<u16>().ok());
        if status != Some(code) {
            return Err(format!("SMTP expected {code}, got {:?}", line.trim_end()));
        }
        if line.as_bytes().get(3) != Some(&b'-') {
            return Ok(());
        }
    }
}

/// A conservative address check: one `@`, a dotted domain, and nothing that
/// could break the SMTP command or header line it is written into.
fn valid_address(address: &str) -> bool {
    address.len() <= 254
        && address.split_once('@').is_some_and(|(local, domain)| {
            !local.is_empty() && domain.contains('.') && !domain.contains('@')
        })
        && address
            .bytes()
            .all(|byte| byte.is_ascii_graphic() && !matches!(byte, b'<' | b'>' | b'\\' | b'"'))
}

fn percent_decode(value: &str) -> Result<Zeroizing<String>, String> {
    let bytes = value.as_bytes();
    let mut decoded = Zeroizing::new(Vec::with_capacity(bytes.len()));
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' {
            let byte = value
                .get(index + 1..index + 3)
                .and_then(|hex| u8::from_str_radix(hex, 16).ok())
                .ok_or("LAYERX_IDENTITY_SMTP_URL has an invalid percent escape")?;
            decoded.push(byte);
            index += 3;
        } else {
            decoded.push(bytes[index]);
            index += 1;
        }
    }
    String::from_utf8(decoded.to_vec())
        .map(Zeroizing::new)
        .map_err(|_| "LAYERX_IDENTITY_SMTP_URL credentials are not UTF-8".to_owned())
}

fn base64(bytes: &[u8]) -> Zeroizing<String> {
    const ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut encoded = Zeroizing::new(String::with_capacity(bytes.len().div_ceil(3) * 4));
    for chunk in bytes.chunks(3) {
        let triple = chunk.iter().enumerate().fold(0_u32, |acc, (i, byte)| {
            acc | u32::from(*byte) << (16 - 8 * i)
        });
        for i in 0..4 {
            if i <= chunk.len() {
                encoded.push(char::from(ALPHABET[(triple >> (18 - 6 * i) & 63) as usize]));
            } else {
                encoded.push('=');
            }
        }
    }
    encoded
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;
    use std::thread;

    /// A minimal SMTP server on loopback: answers one session with the
    /// replies a real server gives and returns every line the client sent.
    fn listener() -> (u16, thread::JoinHandle<Vec<String>>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = thread::spawn(move || {
            let (stream, _) = listener.accept().unwrap();
            let mut writer = stream.try_clone().unwrap();
            let mut reader = BufReader::new(stream);
            let mut lines = Vec::new();
            writer.write_all(b"220 loopback ESMTP\r\n").unwrap();
            let mut in_data = false;
            loop {
                let mut line = String::new();
                if reader.read_line(&mut line).unwrap() == 0 {
                    break;
                }
                let line = line.trim_end_matches("\r\n").to_owned();
                let reply: &[u8] = if in_data {
                    if line == "." {
                        in_data = false;
                        b"250 queued\r\n"
                    } else {
                        b""
                    }
                } else if line.starts_with("EHLO ") {
                    b"250-loopback\r\n250 8BITMIME\r\n"
                } else if line == "DATA" {
                    in_data = true;
                    b"354 go ahead\r\n"
                } else if line == "QUIT" {
                    b"221 bye\r\n"
                } else {
                    b"250 ok\r\n"
                };
                lines.push(line);
                writer.write_all(reply).unwrap();
                if lines.last().is_some_and(|line| line == "QUIT") {
                    break;
                }
            }
            lines
        });
        (port, server)
    }

    #[test]
    fn mailer_delivers_a_queued_verification_to_smtp_and_clears_it() {
        let outbox = std::env::temp_dir().join(format!("mailer-outbox-{}", std::process::id()));
        fs::create_dir_all(&outbox).unwrap();
        fs::write(
            outbox.join("abc.json"),
            br#"{"kind":"signup_verification","email":"ada@example.com","token":"vfy_0123","expires_at":1790000000}"#,
        )
        .unwrap();
        fs::write(outbox.join("abc.tmp"), b"partial").unwrap();
        let (port, server) = listener();
        let smtp = Smtp::parse(
            &format!("smtp://127.0.0.1:{port}"),
            "no-reply@paxeer.network",
        )
        .unwrap();
        assert_eq!(smtp.drain(&outbox).unwrap(), 1);
        let lines = server.join().unwrap();
        assert_eq!(lines[0], "EHLO layerx-identity");
        assert_eq!(lines[1], "MAIL FROM:<no-reply@paxeer.network>");
        assert_eq!(lines[2], "RCPT TO:<ada@example.com>");
        assert_eq!(lines[3], "DATA");
        assert!(lines.contains(&"To: <ada@example.com>".to_owned()));
        assert!(lines.contains(&"Your verification code is vfy_0123".to_owned()));
        assert_eq!(lines[lines.len() - 2], ".");
        assert_eq!(lines[lines.len() - 1], "QUIT");
        assert!(!outbox.join("abc.json").exists());
        assert!(outbox.join("abc.tmp").exists());
        fs::remove_dir_all(&outbox).unwrap();
    }

    #[test]
    fn mailer_keeps_the_verification_queued_when_smtp_refuses() {
        let outbox = std::env::temp_dir().join(format!("mailer-refused-{}", std::process::id()));
        fs::create_dir_all(&outbox).unwrap();
        fs::write(
            outbox.join("abc.json"),
            br#"{"kind":"signup_verification","email":"ada@example.com","token":"vfy_0123","expires_at":1}"#,
        )
        .unwrap();
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream.write_all(b"554 no service\r\n").unwrap();
        });
        let smtp = Smtp::parse(&format!("smtp://127.0.0.1:{port}"), "a@b.co").unwrap();
        assert!(smtp.drain(&outbox).unwrap_err().contains("554"));
        server.join().unwrap();
        assert!(outbox.join("abc.json").exists());
        fs::remove_dir_all(&outbox).unwrap();
    }

    #[test]
    fn mailer_url_parsing_is_exact() {
        let smtps = Smtp::parse("smtps://user%40x:p%3Ass@mail.example.com", "a@b.co").unwrap();
        assert!(smtps.tls);
        assert_eq!(smtps.port, 465);
        assert_eq!(smtps.host, "mail.example.com");
        let (user, password) = smtps.credentials.unwrap();
        assert_eq!((user.as_str(), password.as_str()), ("user@x", "p:ss"));
        assert_eq!(Smtp::parse("smtp://relay", "a@b.co").unwrap().port, 25);
        assert!(Smtp::parse("smtp://u:p@relay", "a@b.co").is_err());
        assert!(Smtp::parse("http://relay", "a@b.co").is_err());
        assert!(Smtp::parse("smtp://relay:x", "a@b.co").is_err());
        assert!(Smtp::parse("smtp://relay", "not-an-address").is_err());
        assert_eq!(base64(b"\0user\0pass").as_str(), "AHVzZXIAcGFzcw==");
    }
}
