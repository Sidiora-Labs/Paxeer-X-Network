use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::fmt::Write as _;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};
use std::time::{SystemTime, UNIX_EPOCH};

use serde_json::{Value, json};
use sha2::{Digest, Sha256};

const THRESHOLD: usize = 10;
const DIGEST_PREFIX: &str = "sha256:";
const NPM_REGISTRY_NAME: &str = "npm";

const EXIT_OVER_TEN: u8 = 1;
const EXIT_USAGE: u8 = 2;
const EXIT_MISSING_SNAPSHOT: u8 = 3;
const EXIT_PROVENANCE: u8 = 4;
const EXIT_LOCAL_HELPER: u8 = 5;
const EXIT_INSTALL: u8 = 6;

const SIDES: [Side; 2] = [
    Side {
        label: "seller",
        source: "platform/examples/paid-api/index.mjs",
        manifest: "platform/examples/paid-api/package.json",
    },
    Side {
        label: "buyer",
        source: "platform/examples/buyer-agent/index.mjs",
        manifest: "platform/examples/buyer-agent/package.json",
    },
];

#[derive(Clone, Copy, Debug)]
struct Side {
    label: &'static str,
    source: &'static str,
    manifest: &'static str,
}

#[derive(Debug, PartialEq, Eq)]
struct Refusal {
    code: u8,
    message: String,
}

fn refuse<T>(code: u8, message: impl Into<String>) -> Result<T, Refusal> {
    Err(Refusal {
        code,
        message: message.into(),
    })
}

#[derive(Debug)]
struct Config {
    repo_root: PathBuf,
    snapshot: PathBuf,
    release_source_digest: String,
    out: PathBuf,
    registry: String,
}

const OPTIONS: [(&str, &str); 5] = [
    ("--repo-root", "PAXEER_X_REPO_ROOT"),
    ("--snapshot", "PAXEER_X_PACKAGE_SNAPSHOT"),
    ("--release-source-digest", "PAXEER_X_RELEASE_SOURCE_DIGEST"),
    ("--out", "PAXEER_X_BENCHMARK_OUT"),
    ("--registry", "PAXEER_X_NPM_REGISTRY"),
];

fn parse_config(
    args: &[String],
    env: &dyn Fn(&str) -> Option<String>,
) -> Result<Config, Refusal> {
    let mut given: BTreeMap<&str, String> = BTreeMap::new();
    let mut rest = args.iter();
    while let Some(flag) = rest.next() {
        let Some((name, _)) = OPTIONS.iter().find(|(name, _)| *name == flag.as_str()) else {
            return refuse(EXIT_USAGE, format!("unknown argument {flag}"));
        };
        let Some(value) = rest.next() else {
            return refuse(EXIT_USAGE, format!("{flag} requires a value"));
        };
        if given.insert(*name, value.clone()).is_some() {
            return refuse(EXIT_USAGE, format!("{flag} given more than once"));
        }
    }
    let mut values = Vec::new();
    for (name, variable) in OPTIONS {
        let value = given.remove(name).or_else(|| env(variable));
        match value {
            Some(value) if !value.trim().is_empty() => values.push(value),
            _ => return refuse(EXIT_USAGE, format!("{name} or {variable} is required")),
        }
    }
    let [repo_root, snapshot, digest, out, registry] = <[String; 5]>::try_from(values)
        .map_err(|_| Refusal {
            code: EXIT_USAGE,
            message: "argument set is incomplete".to_owned(),
        })?;
    if !is_hex(&digest, 64) {
        return refuse(
            EXIT_USAGE,
            format!("release source digest must be 64 lowercase hex digits, got {digest}"),
        );
    }
    if !registry.starts_with("https://") {
        return refuse(EXIT_USAGE, format!("registry must be an https URL, got {registry}"));
    }
    Ok(Config {
        repo_root: PathBuf::from(repo_root),
        snapshot: PathBuf::from(snapshot),
        release_source_digest: digest,
        out: PathBuf::from(out),
        registry,
    })
}

fn is_hex(value: &str, len: usize) -> bool {
    value.len() == len && value.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn sha256_hex(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn trim_registry(url: &str) -> &str {
    url.strip_suffix('/').unwrap_or(url)
}

// ---------------------------------------------------------------- counting

#[derive(Debug, Clone, PartialEq, Eq)]
enum Token {
    Word(String),
    Str(String),
    Template { text: String, substituted: bool },
    Regex,
    Punct(char),
}

#[derive(Debug, Default)]
struct Lexed {
    code_lines: BTreeSet<usize>,
    tokens: Vec<Token>,
}

impl Lexed {
    fn line_count(&self) -> usize {
        self.code_lines.len()
    }
}

struct Lexer {
    chars: Vec<char>,
    at: usize,
    line: usize,
    depth: usize,
    templates: Vec<usize>,
    out: Lexed,
}

const REGEX_AFTER_WORDS: [&str; 13] = [
    "return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw",
    "yield", "await",
];

fn lex(source: &str) -> Result<Lexed, String> {
    let mut lexer = Lexer {
        chars: source.chars().collect(),
        at: 0,
        line: 0,
        depth: 0,
        templates: Vec::new(),
        out: Lexed::default(),
    };
    lexer.run()?;
    Ok(lexer.out)
}

impl Lexer {
    fn peek(&self, offset: usize) -> Option<char> {
        self.chars.get(self.at + offset).copied()
    }

    fn bump(&mut self) -> Option<char> {
        let c = self.chars.get(self.at).copied()?;
        self.at += 1;
        if c == '\n' {
            self.line += 1;
        } else if !c.is_whitespace() {
            self.out.code_lines.insert(self.line);
        }
        Some(c)
    }

    fn skip_comment_char(&mut self) -> Option<char> {
        let c = self.chars.get(self.at).copied()?;
        self.at += 1;
        if c == '\n' {
            self.line += 1;
        }
        Some(c)
    }

    fn regex_allowed(&self) -> bool {
        match self.out.tokens.last() {
            None => true,
            Some(Token::Punct(c)) => "(,=:[!&|?{};+-*%<>~^".contains(*c),
            Some(Token::Word(w)) => REGEX_AFTER_WORDS.contains(&w.as_str()),
            Some(_) => false,
        }
    }

    fn run(&mut self) -> Result<(), String> {
        while let Some(c) = self.peek(0) {
            match c {
                c if c.is_whitespace() => {
                    self.skip_comment_char();
                }
                '/' if self.peek(1) == Some('/') => {
                    while let Some(n) = self.peek(0) {
                        if n == '\n' {
                            break;
                        }
                        self.skip_comment_char();
                    }
                }
                '/' if self.peek(1) == Some('*') => self.block_comment()?,
                '/' if self.regex_allowed() => self.regex()?,
                '"' | '\'' => self.string(c)?,
                '`' => {
                    self.bump();
                    self.template()?;
                }
                '{' => {
                    self.bump();
                    self.depth += 1;
                    self.out.tokens.push(Token::Punct('{'));
                }
                '}' if self.templates.last() == Some(&self.depth) => {
                    self.bump();
                    self.templates.pop();
                    self.template()?;
                }
                '}' => {
                    self.bump();
                    self.depth = self.depth.saturating_sub(1);
                    self.out.tokens.push(Token::Punct('}'));
                }
                c if c.is_alphanumeric() || c == '_' || c == '$' => self.word(),
                _ => {
                    self.bump();
                    self.out.tokens.push(Token::Punct(c));
                }
            }
        }
        if self.templates.is_empty() {
            Ok(())
        } else {
            Err("unterminated template substitution".to_owned())
        }
    }

    fn block_comment(&mut self) -> Result<(), String> {
        self.skip_comment_char();
        self.skip_comment_char();
        loop {
            match self.skip_comment_char() {
                None => return Err("unterminated block comment".to_owned()),
                Some('*') if self.peek(0) == Some('/') => {
                    self.skip_comment_char();
                    return Ok(());
                }
                Some(_) => {}
            }
        }
    }

    fn word(&mut self) {
        let mut text = String::new();
        while let Some(c) = self.peek(0) {
            if c.is_alphanumeric() || c == '_' || c == '$' {
                text.push(c);
                self.bump();
            } else {
                break;
            }
        }
        self.out.tokens.push(Token::Word(text));
    }

    fn string(&mut self, quote: char) -> Result<(), String> {
        self.bump();
        let mut text = String::new();
        loop {
            match self.bump() {
                None | Some('\n') => return Err("unterminated string literal".to_owned()),
                Some('\\') => {
                    if let Some(escaped) = self.bump() {
                        text.push(escaped);
                    }
                }
                Some(c) if c == quote => break,
                Some(c) => text.push(c),
            }
        }
        self.out.tokens.push(Token::Str(text));
        Ok(())
    }

    fn template(&mut self) -> Result<(), String> {
        let mut text = String::new();
        loop {
            match self.bump() {
                None => return Err("unterminated template literal".to_owned()),
                Some('\\') => {
                    if let Some(escaped) = self.bump() {
                        text.push(escaped);
                    }
                }
                Some('`') => {
                    self.out.tokens.push(Token::Template {
                        text,
                        substituted: false,
                    });
                    return Ok(());
                }
                Some('$') if self.peek(0) == Some('{') => {
                    self.bump();
                    self.out.tokens.push(Token::Template {
                        text,
                        substituted: true,
                    });
                    self.out.tokens.push(Token::Punct('('));
                    self.templates.push(self.depth);
                    return Ok(());
                }
                Some(c) => text.push(c),
            }
        }
    }

    fn regex(&mut self) -> Result<(), String> {
        self.bump();
        let mut class = false;
        loop {
            match self.bump() {
                None | Some('\n') => return Err("unterminated regular expression".to_owned()),
                Some('\\') => {
                    self.bump();
                }
                Some('[') => class = true,
                Some(']') => class = false,
                Some('/') if !class => break,
                Some(_) => {}
            }
        }
        while self.peek(0).is_some_and(char::is_alphanumeric) {
            self.bump();
        }
        self.out.tokens.push(Token::Regex);
        Ok(())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct Import {
    specifier: String,
    names: Vec<String>,
}

fn imports(tokens: &[Token]) -> Result<Vec<Import>, String> {
    let mut found = Vec::new();
    let mut at = 0;
    while at < tokens.len() {
        let next = tokens.get(at + 1);
        match (&tokens[at], next) {
            (Token::Word(w), Some(Token::Punct('('))) if w == "import" || w == "require" => {
                match tokens.get(at + 2) {
                    Some(Token::Str(s)) => found.push(Import {
                        specifier: s.clone(),
                        names: Vec::new(),
                    }),
                    Some(Token::Template {
                        text,
                        substituted: false,
                    }) => found.push(Import {
                        specifier: text.clone(),
                        names: Vec::new(),
                    }),
                    _ => return Err(format!("{w}() with a computed specifier cannot be bound")),
                }
            }
            (Token::Word(w), Some(Token::Str(s))) if w == "import" => found.push(Import {
                specifier: s.clone(),
                names: Vec::new(),
            }),
            (Token::Word(w), next)
                if (w == "import" || w == "export")
                    && !matches!(next, Some(Token::Punct('.')))
                    && !matches!(at.checked_sub(1).and_then(|p| tokens.get(p)), Some(Token::Punct('.'))) =>
            {
                if let Some(import) = static_from(tokens, at) {
                    found.push(import);
                }
            }
            _ => {}
        }
        at += 1;
    }
    Ok(found)
}

fn static_from(tokens: &[Token], start: usize) -> Option<Import> {
    let mut names = Vec::new();
    let mut braces = false;
    let mut at = start + 1;
    while let Some(token) = tokens.get(at) {
        match token {
            Token::Punct('{') => braces = true,
            Token::Punct('}') => braces = false,
            Token::Punct(';') => return None,
            Token::Word(w) if w == "from" && !braces => {
                return match tokens.get(at + 1) {
                    Some(Token::Str(s)) => Some(Import {
                        specifier: s.clone(),
                        names,
                    }),
                    _ => None,
                };
            }
            Token::Word(w) if braces && w != "as" && w != "type" => {
                if !matches!(tokens.get(at - 1), Some(Token::Word(prev)) if prev == "as") {
                    names.push(w.clone());
                }
            }
            Token::Word(w)
                if !braces && matches!(w.as_str(), "const" | "let" | "var" | "function" | "class" | "default" | "async") =>
            {
                return None;
            }
            _ => {}
        }
        at += 1;
    }
    None
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Specifier {
    Builtin,
    Package { name: String },
}

fn classify(specifier: &str) -> Result<Specifier, String> {
    if specifier.starts_with("node:") {
        return Ok(Specifier::Builtin);
    }
    let local = specifier.starts_with('.')
        || specifier.starts_with('/')
        || specifier.starts_with('#')
        || specifier.contains(':')
        || specifier.contains('\\');
    if local || specifier.is_empty() {
        return Err(format!(
            "local or workspace helper import {specifier} is not a published package"
        ));
    }
    let mut parts = specifier.split('/');
    let name = match (parts.next(), parts.next()) {
        (Some(scope), Some(bare)) if scope.starts_with('@') && !bare.is_empty() => {
            format!("{scope}/{bare}")
        }
        (Some(bare), _) if !bare.starts_with('@') => bare.to_owned(),
        _ => return Err(format!("malformed package specifier {specifier}")),
    };
    Ok(Specifier::Package { name })
}

// ---------------------------------------------------------------- snapshot

struct Snapshot {
    manifest: Value,
    publication: BTreeMap<String, String>,
    digests: BTreeMap<String, String>,
    install: PathBuf,
    lock: Value,
}

fn required_file(path: &Path) -> Result<Vec<u8>, Refusal> {
    match fs::read(path) {
        Ok(bytes) if !bytes.iter().all(u8::is_ascii_whitespace) => Ok(bytes),
        Ok(_) => refuse(EXIT_MISSING_SNAPSHOT, format!("{} is empty", path.display())),
        Err(e) => refuse(EXIT_MISSING_SNAPSHOT, format!("{} is unavailable: {e}", path.display())),
    }
}

fn parse_json(bytes: &[u8], path: &Path) -> Result<Value, Refusal> {
    serde_json::from_slice(bytes).map_err(|e| Refusal {
        code: EXIT_PROVENANCE,
        message: format!("{} is not valid JSON: {e}", path.display()),
    })
}

fn load_snapshot(dir: &Path) -> Result<Snapshot, Refusal> {
    if !dir.is_dir() {
        return refuse(EXIT_MISSING_SNAPSHOT, format!("snapshot {} does not exist", dir.display()));
    }
    let npm = dir.join("npm");
    let install = npm.join("install");
    let manifest_path = dir.join("artifact-manifest.json");
    let manifest = parse_json(&required_file(&manifest_path)?, &manifest_path)?;
    let publication_text = String::from_utf8_lossy(&required_file(&npm.join("publication.txt"))?).into_owned();
    let digests_text = String::from_utf8_lossy(&required_file(&npm.join("digests.txt"))?).into_owned();
    required_file(&install.join("package.json"))?;
    let lock_path = install.join("package-lock.json");
    let lock = parse_json(&required_file(&lock_path)?, &lock_path)?;
    let registry_dir = install.join("registry");
    let tarballs = fs::read_dir(&registry_dir)
        .map(|entries| {
            entries
                .filter_map(Result::ok)
                .filter(|e| e.path().extension().is_some_and(|x| x == "tgz"))
                .count()
        })
        .unwrap_or(0);
    if tarballs == 0 {
        return refuse(
            EXIT_MISSING_SNAPSHOT,
            format!("{} holds no registry-fetched tarballs", registry_dir.display()),
        );
    }
    Ok(Snapshot {
        manifest,
        publication: key_values(&publication_text),
        digests: digest_lines(&digests_text)?,
        install,
        lock,
    })
}

fn key_values(text: &str) -> BTreeMap<String, String> {
    text.lines()
        .filter_map(|line| line.split_once('='))
        .map(|(k, v)| (k.trim().to_owned(), v.trim().to_owned()))
        .collect()
}

fn digest_lines(text: &str) -> Result<BTreeMap<String, String>, Refusal> {
    let mut digests = BTreeMap::new();
    for line in text.lines().filter(|l| !l.trim().is_empty()) {
        let Some((digest, file)) = line.split_once(char::is_whitespace) else {
            return refuse(EXIT_PROVENANCE, format!("malformed digests.txt line {line}"));
        };
        let file = file.trim_start().trim_start_matches('*');
        if !is_hex(digest, 64) || file.is_empty() {
            return refuse(EXIT_PROVENANCE, format!("malformed digests.txt line {line}"));
        }
        if digests.insert(file.to_owned(), digest.to_owned()).is_some() {
            return refuse(EXIT_PROVENANCE, format!("digests.txt lists {file} twice"));
        }
    }
    Ok(digests)
}

fn publication_checks(snapshot: &Snapshot, registry: &str) -> Result<String, Refusal> {
    let field = |key: &str| snapshot.publication.get(key).map(String::as_str);
    if field("published") != Some("true") {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt does not record published=true");
    }
    if field("install_check") != Some("pass") {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt does not record install_check=pass");
    }
    if field("registry") != Some(NPM_REGISTRY_NAME) {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt does not record registry=npm");
    }
    let Some(target) = field("target") else {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt does not record the target registry");
    };
    if trim_registry(target) != trim_registry(registry) {
        return refuse(
            EXIT_PROVENANCE,
            format!("configured registry {registry} differs from the published target {target}"),
        );
    }
    let Some(version) = field("version").filter(|v| !v.is_empty()) else {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt does not record the version");
    };
    Ok(version.to_owned())
}

fn manifest_revision(snapshot: &Snapshot, release_source_digest: &str) -> Result<String, Refusal> {
    let source_digest = snapshot.manifest.get("source_digest").and_then(Value::as_str);
    if source_digest != Some(format!("{DIGEST_PREFIX}{release_source_digest}").as_str()) {
        return refuse(
            EXIT_PROVENANCE,
            format!(
                "artifact manifest source_digest {} is not {DIGEST_PREFIX}{release_source_digest}",
                source_digest.unwrap_or("<absent>")
            ),
        );
    }
    let revision = snapshot.manifest.get("source_revision").and_then(Value::as_str).unwrap_or("");
    if !is_hex(revision, 40) {
        return refuse(EXIT_PROVENANCE, format!("artifact manifest source_revision {revision} is not a commit"));
    }
    if snapshot.publication.get("revision").map(String::as_str) != Some(revision) {
        return refuse(EXIT_PROVENANCE, "npm/publication.txt revision differs from the manifest source_revision");
    }
    Ok(revision.to_owned())
}

fn git(repo: &Path, args: &[&str]) -> Result<Vec<u8>, Refusal> {
    let output = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .stdin(Stdio::null())
        .output()
        .map_err(|e| Refusal {
            code: EXIT_PROVENANCE,
            message: format!("git {} could not start: {e}", args.join(" ")),
        })?;
    if output.status.success() {
        Ok(output.stdout)
    } else {
        refuse(
            EXIT_PROVENANCE,
            format!("git {} failed: {}", args.join(" "), String::from_utf8_lossy(&output.stderr).trim()),
        )
    }
}

fn verify_source_archive(repo: &Path, revision: &str, expected: &str) -> Result<(), Refusal> {
    let mut child = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(["archive", "--format=tar", revision])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|e| Refusal {
            code: EXIT_PROVENANCE,
            message: format!("git archive could not start: {e}"),
        })?;
    let mut hasher = Sha256::new();
    if let Some(mut stdout) = child.stdout.take() {
        let mut buffer = vec![0_u8; 1 << 16];
        loop {
            let read = stdout.read(&mut buffer).map_err(|e| Refusal {
                code: EXIT_PROVENANCE,
                message: format!("git archive output could not be read: {e}"),
            })?;
            if read == 0 {
                break;
            }
            hasher.update(&buffer[..read]);
        }
    }
    let status = child.wait().map_err(|e| Refusal {
        code: EXIT_PROVENANCE,
        message: format!("git archive did not finish: {e}"),
    })?;
    let actual = format!("{:x}", hasher.finalize());
    if !status.success() {
        return refuse(EXIT_PROVENANCE, format!("git archive {revision} failed"));
    }
    if actual != expected {
        return refuse(
            EXIT_PROVENANCE,
            format!("source archive of {revision} hashes to {actual}, not the release source digest {expected}"),
        );
    }
    Ok(())
}

// ---------------------------------------------------------------- binding

#[derive(Debug, Clone, PartialEq, Eq)]
struct BoundPackage {
    name: String,
    version: String,
    integrity: String,
    names: BTreeSet<String>,
}

#[derive(Debug)]
struct Measured {
    side: Side,
    sha256: String,
    lines: usize,
    packages: Vec<BoundPackage>,
}

fn measure_source(side: Side, source: &[u8]) -> Result<(usize, Vec<Import>), Refusal> {
    let text = std::str::from_utf8(source).map_err(|_| Refusal {
        code: EXIT_PROVENANCE,
        message: format!("{} is not UTF-8", side.source),
    })?;
    let lexed = lex(text).map_err(|e| Refusal {
        code: EXIT_PROVENANCE,
        message: format!("{} cannot be counted: {e}", side.source),
    })?;
    if lexed.line_count() == 0 {
        return refuse(EXIT_PROVENANCE, format!("{} holds no integration code", side.source));
    }
    let found = imports(&lexed.tokens).map_err(|e| Refusal {
        code: EXIT_LOCAL_HELPER,
        message: format!("{}: {e}", side.source),
    })?;
    for import in &found {
        classify(&import.specifier).map_err(|e| Refusal {
            code: EXIT_LOCAL_HELPER,
            message: format!("{}: {e}", side.source),
        })?;
    }
    Ok((lexed.line_count(), found))
}

fn pinned_dependencies(side: Side, manifest: &[u8]) -> Result<BTreeMap<String, String>, Refusal> {
    let value = parse_json(manifest, Path::new(side.manifest))?;
    let Some(dependencies) = value.get("dependencies").and_then(Value::as_object) else {
        return refuse(EXIT_PROVENANCE, format!("{} declares no dependencies", side.manifest));
    };
    let mut pinned = BTreeMap::new();
    for (name, version) in dependencies {
        let Some(version) = version.as_str() else {
            return refuse(EXIT_PROVENANCE, format!("{} pins {name} to a non-string", side.manifest));
        };
        if version.is_empty() || !version.chars().all(|c| c.is_ascii_alphanumeric() || ".-+".contains(c)) {
            return refuse(EXIT_LOCAL_HELPER, format!("{} resolves {name} to {version}", side.manifest));
        }
        pinned.insert(name.clone(), version.to_owned());
    }
    Ok(pinned)
}

fn bind_packages(
    side: Side,
    found: &[Import],
    pinned: &BTreeMap<String, String>,
    release_version: &str,
) -> Result<Vec<(String, String, BTreeSet<String>)>, Refusal> {
    let mut bound: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
    for import in found {
        if let Specifier::Package { name } = classify(&import.specifier).map_err(|e| Refusal {
            code: EXIT_LOCAL_HELPER,
            message: e,
        })? {
            bound.entry(name).or_default().extend(import.names.iter().cloned());
        }
    }
    if bound.is_empty() {
        return refuse(EXIT_PROVENANCE, format!("{} imports no published package", side.source));
    }
    let mut out = Vec::new();
    for (name, names) in bound {
        let Some(version) = pinned.get(&name) else {
            return refuse(EXIT_PROVENANCE, format!("{} imports {name}, which {} does not pin", side.source, side.manifest));
        };
        if version != release_version {
            return refuse(
                EXIT_PROVENANCE,
                format!("{} pins {name}@{version}, the snapshot publishes {release_version}"),
            );
        }
        out.push((name, version.clone(), names));
    }
    Ok(out)
}

fn tarball_name(name: &str, version: &str) -> String {
    format!("{}-{version}.tgz", name.trim_start_matches('@').replace('/', "-"))
}

fn verify_published(snapshot: &Snapshot, revision: &str, name: &str, version: &str) -> Result<(), Refusal> {
    let tarball = tarball_name(name, version);
    let Some(recorded) = snapshot.digests.get(&tarball) else {
        return refuse(EXIT_PROVENANCE, format!("digests.txt does not record {tarball}"));
    };
    let path = snapshot.install.join("registry").join(&tarball);
    let bytes = fs::read(&path).map_err(|e| Refusal {
        code: EXIT_MISSING_SNAPSHOT,
        message: format!("registry-fetched tarball {} is unavailable: {e}", path.display()),
    })?;
    let fetched = sha256_hex(&bytes);
    if &fetched != recorded {
        return refuse(EXIT_PROVENANCE, format!("registry bytes of {tarball} hash to {fetched}, digests.txt records {recorded}"));
    }
    let entries = snapshot.manifest.get("artifacts").and_then(Value::as_array).map_or(&[][..], Vec::as_slice);
    let matching: Vec<&Value> = entries
        .iter()
        .filter(|e| e.get("name").and_then(Value::as_str) == Some(name) && e.get("registry").and_then(Value::as_str) == Some(NPM_REGISTRY_NAME))
        .collect();
    let [entry] = matching.as_slice() else {
        return refuse(EXIT_PROVENANCE, format!("artifact manifest holds {} npm records for {name}", matching.len()));
    };
    let text = |key: &str| entry.get(key).and_then(Value::as_str).unwrap_or("");
    let flag = |key: &str| entry.get(key).and_then(Value::as_bool) == Some(true);
    if text("version") != version
        || text("source_revision") != revision
        || text("digest") != format!("{DIGEST_PREFIX}{fetched}")
        || !flag("published")
        || !flag("install_check")
    {
        return refuse(
            EXIT_PROVENANCE,
            format!("artifact manifest record of {name} is not the published, install-checked {version} at {revision} with digest {DIGEST_PREFIX}{fetched}"),
        );
    }
    Ok(())
}

fn lock_packages(lock: &Value) -> Result<&serde_json::Map<String, Value>, Refusal> {
    lock.get("packages").and_then(Value::as_object).ok_or_else(|| Refusal {
        code: EXIT_PROVENANCE,
        message: "npm/install/package-lock.json has no packages map".to_owned(),
    })
}

fn lock_integrity(lock: &Value, registry: &str, name: &str, version: &str) -> Result<String, Refusal> {
    let packages = lock_packages(lock)?;
    let Some(entry) = packages.get(&format!("node_modules/{name}")) else {
        return refuse(EXIT_PROVENANCE, format!("installed resolution does not hold {name}"));
    };
    let text = |key: &str| entry.get(key).and_then(Value::as_str).unwrap_or("");
    let resolved_prefix = format!("{}/", trim_registry(registry));
    if text("version") != version || !text("resolved").starts_with(&resolved_prefix) {
        return refuse(
            EXIT_PROVENANCE,
            format!("installed {name} is {}@{} from {}, not {version} from {resolved_prefix}", name, text("version"), text("resolved")),
        );
    }
    let integrity = text("integrity");
    if !integrity.starts_with("sha512-") {
        return refuse(EXIT_PROVENANCE, format!("installed {name} carries no sha512 integrity"));
    }
    Ok(integrity.to_owned())
}

fn check_closure(lock: &Value, registry: &str) -> Result<(), Refusal> {
    let packages = lock_packages(lock)?;
    let resolved_prefix = format!("{}/", trim_registry(registry));
    for (key, entry) in packages {
        if !key.is_empty() {
            let resolved = entry.get("resolved").and_then(Value::as_str).unwrap_or("");
            let integrity = entry.get("integrity").and_then(Value::as_str).unwrap_or("");
            if entry.get("link").and_then(Value::as_bool) == Some(true)
                || !resolved.starts_with(&resolved_prefix)
                || integrity.is_empty()
            {
                return refuse(EXIT_PROVENANCE, format!("{key} is not a registry tarball with integrity (resolved {resolved})"));
            }
        }
        let dependencies = entry.get("dependencies").and_then(Value::as_object);
        for dependency in dependencies.into_iter().flat_map(serde_json::Map::keys) {
            if !resolves(packages, key, dependency) {
                return refuse(EXIT_PROVENANCE, format!("dependency closure gap: {dependency} of {} is not installed", if key.is_empty() { "<root>" } else { key }));
            }
        }
    }
    Ok(())
}

fn resolves(packages: &serde_json::Map<String, Value>, from: &str, dependency: &str) -> bool {
    let mut base = from;
    loop {
        let candidate = if base.is_empty() {
            format!("node_modules/{dependency}")
        } else {
            format!("{base}/node_modules/{dependency}")
        };
        if packages.contains_key(&candidate) {
            return true;
        }
        if base.is_empty() {
            return false;
        }
        base = base.rfind("/node_modules/").map_or("", |at| &base[..at]);
        if base == "node_modules" {
            base = "";
        }
    }
}

fn bind_side(
    config: &Config,
    snapshot: &Snapshot,
    revision: &str,
    release_version: &str,
    side: Side,
) -> Result<Measured, Refusal> {
    let source = git(&config.repo_root, &["show", &format!("{revision}:{}", side.source)])?;
    let manifest = git(&config.repo_root, &["show", &format!("{revision}:{}", side.manifest)])?;
    let (lines, found) = measure_source(side, &source)?;
    let pinned = pinned_dependencies(side, &manifest)?;
    let mut packages = Vec::new();
    for (name, version, names) in bind_packages(side, &found, &pinned, release_version)? {
        verify_published(snapshot, revision, &name, &version)?;
        let integrity = lock_integrity(&snapshot.lock, &config.registry, &name, &version)?;
        packages.push(BoundPackage {
            name,
            version,
            integrity,
            names,
        });
    }
    Ok(Measured {
        side,
        sha256: sha256_hex(&source),
        lines,
        packages,
    })
}

// ---------------------------------------------------------------- isolated install

fn install_failure(message: impl Into<String>) -> Refusal {
    Refusal {
        code: EXIT_INSTALL,
        message: message.into(),
    }
}

fn isolated_dir(repo_root: &Path) -> Result<PathBuf, Refusal> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map_or(0, |d| d.as_nanos());
    let dir = std::env::temp_dir().join(format!("layerx-ten-line-benchmark-{}-{nanos}", std::process::id()));
    fs::create_dir_all(&dir).map_err(|e| install_failure(format!("cannot create {}: {e}", dir.display())))?;
    let dir = dir.canonicalize().map_err(|e| install_failure(format!("cannot resolve {}: {e}", dir.display())))?;
    if let Ok(root) = repo_root.canonicalize() {
        if dir.starts_with(&root) {
            return Err(install_failure("isolated consumer directory lies inside the repository"));
        }
    }
    Ok(dir)
}

fn run_tool(program: &str, args: &[&str], dir: &Path, registry: &str) -> Result<(), Refusal> {
    let npmrc = dir.join("isolated.npmrc");
    let output = Command::new(program)
        .args(args)
        .current_dir(dir)
        .env_remove("NODE_PATH")
        .env_remove("NODE_OPTIONS")
        .env("NPM_CONFIG_USERCONFIG", &npmrc)
        .env("NPM_CONFIG_GLOBALCONFIG", &npmrc)
        .env("NPM_CONFIG_CACHE", dir.join(".npm-cache"))
        .env("NPM_CONFIG_REGISTRY", registry)
        .stdin(Stdio::null())
        .output()
        .map_err(|e| install_failure(format!("{program} could not start: {e}")))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(install_failure(format!(
            "{program} {} exited {}: {}",
            args.join(" "),
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        )))
    }
}

fn probe_script(packages: &[&BoundPackage], dir: &Path) -> Result<String, Refusal> {
    let root = serde_json::to_string(&format!("{}/node_modules/", dir.display()))
        .map_err(|e| install_failure(format!("{e}")))?;
    let mut script = format!("import {{ fileURLToPath }} from \"node:url\";\nconst root = {root};\n");
    for package in packages {
        let name = serde_json::to_string(&package.name).map_err(|e| install_failure(format!("{e}")))?;
        let names = serde_json::to_string(&package.names).map_err(|e| install_failure(format!("{e}")))?;
        writeln!(
            script,
            "{{ const at = fileURLToPath(import.meta.resolve({name})); if (!at.startsWith(root)) {{ console.error(`${{{name}}} resolved outside the isolated install: ${{at}}`); process.exit(1); }}\n  const module = await import({name}); for (const n of {names}) if (!(n in module)) {{ console.error(`${{{name}}} does not export ${{n}}`); process.exit(1); }} }}"
        )
        .map_err(|e| install_failure(format!("{e}")))?;
    }
    Ok(script)
}

fn isolated_install(config: &Config, snapshot: &Snapshot, measured: &[Measured]) -> Result<(), Refusal> {
    let dir = isolated_dir(&config.repo_root)?;
    let result = install_into(&dir, config, snapshot, measured);
    let cleaned = fs::remove_dir_all(&dir);
    result?;
    cleaned.map_err(|e| install_failure(format!("cannot remove {}: {e}", dir.display())))
}

fn install_into(dir: &Path, config: &Config, snapshot: &Snapshot, measured: &[Measured]) -> Result<(), Refusal> {
    {
        for file in ["package.json", "package-lock.json"] {
            fs::copy(snapshot.install.join(file), dir.join(file))
                .map_err(|e| install_failure(format!("cannot stage {file}: {e}")))?;
        }
        fs::write(dir.join("isolated.npmrc"), format!("registry={}\n", config.registry))
            .map_err(|e| install_failure(format!("cannot write isolated npmrc: {e}")))?;
        run_tool(
            "npm",
            &["ci", "--ignore-scripts", "--no-audit", "--no-fund", "--workspaces=false", "--registry", &config.registry],
            dir,
            &config.registry,
        )?;
        let packages: Vec<&BoundPackage> = measured.iter().flat_map(|m| m.packages.iter()).collect();
        for package in &packages {
            let installed = dir.join("node_modules").join(&package.name).join("package.json");
            let value = fs::read(&installed)
                .ok()
                .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok());
            let version = value.as_ref().and_then(|v| v.get("version")).and_then(Value::as_str);
            if version != Some(package.version.as_str()) {
                return Err(install_failure(format!("isolated install did not place {}@{}", package.name, package.version)));
            }
        }
        fs::write(dir.join("probe.mjs"), probe_script(&packages, dir)?)
            .map_err(|e| install_failure(format!("cannot write import probe: {e}")))?;
        run_tool("node", &["probe.mjs"], dir, &config.registry)
    }
}

// ---------------------------------------------------------------- artifact

fn artifact(config: &Config, measured: &[Measured], passed: bool) -> Value {
    let mut document = json!({
        "threshold": THRESHOLD,
        "release_source_digest": config.release_source_digest,
        "registry": config.registry,
        "passed": passed,
    });
    for m in measured {
        let packages: Vec<Value> = m
            .packages
            .iter()
            .map(|p| json!({"name": p.name, "version": p.version, "integrity": p.integrity}))
            .collect();
        document[m.side.label] = json!({
            "source": m.side.source,
            "sha256": m.sha256,
            "lines": m.lines,
            "packages": packages,
        });
    }
    document
}

fn write_artifact(path: &Path, document: &Value) -> Result<(), Refusal> {
    let fail = |e: std::io::Error| Refusal {
        code: EXIT_USAGE,
        message: format!("cannot write {}: {e}", path.display()),
    };
    if let Some(parent) = path.parent().filter(|p| !p.as_os_str().is_empty()) {
        fs::create_dir_all(parent).map_err(fail)?;
    }
    let mut text = serde_json::to_string_pretty(document).map_err(|e| Refusal {
        code: EXIT_USAGE,
        message: e.to_string(),
    })?;
    text.push('\n');
    fs::write(path, text).map_err(fail)
}

fn run(config: &Config) -> Result<Vec<Measured>, Refusal> {
    let snapshot = load_snapshot(&config.snapshot)?;
    let release_version = publication_checks(&snapshot, &config.registry)?;
    let revision = manifest_revision(&snapshot, &config.release_source_digest)?;
    verify_source_archive(&config.repo_root, &revision, &config.release_source_digest)?;
    let mut measured = Vec::new();
    for side in SIDES {
        measured.push(bind_side(config, &snapshot, &revision, &release_version, side)?);
    }
    if measured[0].sha256 == measured[1].sha256 {
        return refuse(EXIT_PROVENANCE, "seller and buyer quickstarts are the same source");
    }
    check_closure(&snapshot.lock, &config.registry)?;
    isolated_install(config, &snapshot, &measured)?;
    Ok(measured)
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let outcome = parse_config(&args, &|name| std::env::var(name).ok()).and_then(|config| {
        let measured = run(&config)?;
        let passed = measured.iter().all(|m| m.lines <= THRESHOLD);
        write_artifact(&config.out, &artifact(&config, &measured, passed))?;
        for m in &measured {
            println!("{} {} {} lines (threshold {THRESHOLD})", m.side.label, m.side.source, m.lines);
        }
        Ok(passed)
    });
    match outcome {
        Ok(true) => ExitCode::SUCCESS,
        Ok(false) => {
            eprintln!("ten-line benchmark failed: an integration exceeds {THRESHOLD} lines");
            ExitCode::from(EXIT_OVER_TEN)
        }
        Err(refusal) => {
            eprintln!("ten-line benchmark refused: {}", refusal.message);
            ExitCode::from(refusal.code)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn count(source: &str) -> Result<usize, String> {
        lex(source).map(|l| l.line_count())
    }

    fn scratch(name: &str) -> Result<PathBuf, String> {
        let dir = std::env::temp_dir().join(format!("layerx-benchmark-test-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(dir.join("npm/install/registry")).map_err(|e| format!("{e}"))?;
        Ok(dir)
    }

    const SELLER: Side = SIDES[0];

    #[test]
    fn counts_code_lines_and_skips_blank_and_comment_lines() -> Result<(), String> {
        let source = "// header\n\nimport { A } from \"pkg\";\n/* block\n   comment */\nconst a = 1; // trailing\n   \n/** doc */ a;\n";
        assert_eq!(count(source)?, 3);
        Ok(())
    }

    #[test]
    fn counts_every_physical_line_of_multi_line_constructs() -> Result<(), String> {
        let source = "import {\n  A,\n  B,\n} from \"pkg\";\nconst t = `one\n\ntwo ${a +\n b}`;\nconst o = {\n  k: \"// not a comment\",\n};\n";
        assert_eq!(count(source)?, 10);
        Ok(())
    }

    #[test]
    fn comment_markers_inside_strings_and_regex_are_code() -> Result<(), String> {
        assert_eq!(count("const u = \"http://x/*y\";\nconst r = /\\/\\*[/]/u.test(u);\n")?, 2);
        assert_eq!(count("const half = a / b; // c\n")?, 1);
        Ok(())
    }

    #[test]
    fn compacted_semicolon_lines_count_once_per_physical_line() -> Result<(), String> {
        assert_eq!(count("a(); b(); c(); d(); e(); f(); g(); h(); i(); j(); k(); l();\n")?, 1);
        Ok(())
    }

    #[test]
    fn unterminated_constructs_are_refused() {
        assert!(count("/* open").is_err());
        assert!(count("const s = \"open\n").is_err());
        assert!(count("const t = `open ${a").is_err());
    }

    #[test]
    fn detects_static_dynamic_and_require_imports() -> Result<(), String> {
        let lexed = lex("import { A, B as C } from \"@s/p\";\nimport \"side\";\nimport * as ns from 'other';\nexport { X } from \"re/sub\";\nconst m = await import(\"dyn\");\nconst r = require(`cjs`);\nconst z = from;\n")?;
        let found = imports(&lexed.tokens)?;
        let specs: Vec<&str> = found.iter().map(|i| i.specifier.as_str()).collect();
        assert_eq!(specs, ["@s/p", "side", "other", "re/sub", "dyn", "cjs"]);
        assert_eq!(found[0].names, ["A", "B"]);
        assert_eq!(classify("re/sub"), Ok(Specifier::Package { name: "re".to_owned() }));
        assert_eq!(classify("@s/p/deep"), Ok(Specifier::Package { name: "@s/p".to_owned() }));
        assert_eq!(classify("node:http"), Ok(Specifier::Builtin));
        Ok(())
    }

    #[test]
    fn computed_dynamic_import_is_refused() -> Result<(), String> {
        let lexed = lex("const m = await import(name);\n")?;
        assert!(imports(&lexed.tokens).is_err());
        Ok(())
    }

    #[test]
    fn local_helper_imports_are_refused() {
        for specifier in ["../support/runtime.mjs", "./helper.mjs", "/abs/x.mjs", "file:../x", "link:../x", "workspace:*", "#internal"] {
            assert!(classify(specifier).is_err(), "{specifier}");
        }
        let source = b"import { SellerMiddleware } from \"@sidiora/layerx-seller-middleware\";\nimport { requiredEnvironment } from \"../support/runtime.mjs\";\n";
        let refused = measure_source(SELLER, source).err().map(|r| r.code);
        assert_eq!(refused, Some(EXIT_LOCAL_HELPER));
    }

    #[test]
    fn workspace_dependency_pins_are_refused() {
        let refused = pinned_dependencies(SELLER, br#"{"dependencies":{"@sidiora/layerx-sdk":"workspace:*"}}"#).err().map(|r| r.code);
        assert_eq!(refused, Some(EXIT_LOCAL_HELPER));
        let refused = pinned_dependencies(SELLER, br#"{"dependencies":{"@sidiora/layerx-sdk":"file:../../sdk"}}"#).err().map(|r| r.code);
        assert_eq!(refused, Some(EXIT_LOCAL_HELPER));
    }

    #[test]
    fn empty_source_is_refused_not_counted_as_zero() {
        assert_eq!(measure_source(SELLER, b"\n// only a comment\n").err().map(|r| r.code), Some(EXIT_PROVENANCE));
    }

    #[test]
    fn missing_arguments_and_bad_digest_are_usage_errors() {
        let none = |_: &str| -> Option<String> { None };
        assert_eq!(parse_config(&[], &none).err().map(|r| r.code), Some(EXIT_USAGE));
        let args: Vec<String> = ["--repo-root", ".", "--snapshot", "s", "--release-source-digest", "ABC", "--out", "o", "--registry", "https://registry.npmjs.org/"]
            .iter()
            .map(|s| (*s).to_owned())
            .collect();
        assert_eq!(parse_config(&args, &none).err().map(|r| r.code), Some(EXIT_USAGE));
    }

    #[test]
    fn flags_win_over_environment() -> Result<(), String> {
        let digest = "a".repeat(64);
        let env = |name: &str| match name {
            "PAXEER_X_REPO_ROOT" => Some("/env-root".to_owned()),
            "PAXEER_X_PACKAGE_SNAPSHOT" => Some("/snap".to_owned()),
            "PAXEER_X_RELEASE_SOURCE_DIGEST" => Some("a".repeat(64)),
            "PAXEER_X_BENCHMARK_OUT" => Some("/out.json".to_owned()),
            "PAXEER_X_NPM_REGISTRY" => Some("https://registry.npmjs.org/".to_owned()),
            _ => None,
        };
        let config = parse_config(&["--repo-root".to_owned(), "/flag-root".to_owned()], &env).map_err(|r| r.message)?;
        assert_eq!(config.repo_root, PathBuf::from("/flag-root"));
        assert_eq!(config.release_source_digest, digest);
        Ok(())
    }

    #[test]
    fn missing_snapshot_is_refused() -> Result<(), String> {
        let missing = std::env::temp_dir().join("layerx-benchmark-test-absent-snapshot");
        assert_eq!(load_snapshot(&missing).err().map(|r| r.code), Some(EXIT_MISSING_SNAPSHOT));
        let dir = scratch("partial")?;
        fs::write(dir.join("artifact-manifest.json"), "{}").map_err(|e| format!("{e}"))?;
        fs::write(dir.join("npm/publication.txt"), "").map_err(|e| format!("{e}"))?;
        assert_eq!(load_snapshot(&dir).err().map(|r| r.code), Some(EXIT_MISSING_SNAPSHOT));
        fs::remove_dir_all(&dir).map_err(|e| format!("{e}"))
    }

    fn fixture_snapshot(name: &str, tarball: &[u8], recorded: &str) -> Result<(PathBuf, Snapshot), String> {
        let dir = scratch(name)?;
        let revision = "b".repeat(40);
        let file = tarball_name("@sidiora/layerx-sdk", "0.1.0");
        let write = |path: &str, text: &[u8]| fs::write(dir.join(path), text).map_err(|e| format!("{e}"));
        write(
            "artifact-manifest.json",
            json!({
                "source_digest": format!("sha256:{}", "c".repeat(64)),
                "source_revision": revision,
                "artifacts": [{"name": "@sidiora/layerx-sdk", "version": "0.1.0", "registry": "npm", "source_revision": revision, "digest": format!("sha256:{}", sha256_hex(tarball)), "published": true, "install_check": true}],
            })
            .to_string()
            .as_bytes(),
        )?;
        write("npm/publication.txt", format!("registry=npm\nversion=0.1.0\ntarget=https://registry.npmjs.org\nrevision={revision}\npublished=true\ninstall_check=pass\n").as_bytes())?;
        write("npm/digests.txt", format!("{recorded}  {file}\n").as_bytes())?;
        write("npm/install/package.json", br#"{"dependencies":{"@sidiora/layerx-sdk":"0.1.0"}}"#)?;
        write(
            "npm/install/package-lock.json",
            json!({"packages": {
                "": {"dependencies": {"@sidiora/layerx-sdk": "0.1.0"}},
                "node_modules/@sidiora/layerx-sdk": {"version": "0.1.0", "resolved": "https://registry.npmjs.org/@sidiora/layerx-sdk/-/layerx-sdk-0.1.0.tgz", "integrity": "sha512-AAAA", "dependencies": {"left-pad": "^1.0.0"}},
            }})
            .to_string()
            .as_bytes(),
        )?;
        fs::write(dir.join("npm/install/registry").join(&file), tarball).map_err(|e| format!("{e}"))?;
        let snapshot = load_snapshot(&dir).map_err(|r| r.message)?;
        Ok((dir, snapshot))
    }

    #[test]
    fn registry_and_digest_mismatches_are_refused() -> Result<(), String> {
        let tarball = b"registry bytes";
        let (dir, snapshot) = fixture_snapshot("provenance", tarball, &sha256_hex(tarball))?;
        assert_eq!(publication_checks(&snapshot, "https://registry.npmjs.org/"), Ok("0.1.0".to_owned()));
        assert_eq!(publication_checks(&snapshot, "https://npm.example.invalid/").err().map(|r| r.code), Some(EXIT_PROVENANCE));
        assert_eq!(manifest_revision(&snapshot, &"d".repeat(64)).err().map(|r| r.code), Some(EXIT_PROVENANCE));
        assert_eq!(manifest_revision(&snapshot, &"c".repeat(64)), Ok("b".repeat(40)));
        assert_eq!(verify_published(&snapshot, &"b".repeat(40), "@sidiora/layerx-sdk", "0.1.0"), Ok(()));
        assert_eq!(verify_published(&snapshot, &"b".repeat(40), "@sidiora/layerx-sdk", "0.2.0").err().map(|r| r.code), Some(EXIT_PROVENANCE));
        assert_eq!(lock_integrity(&snapshot.lock, "https://registry.npmjs.org/", "@sidiora/layerx-sdk", "0.1.0"), Ok("sha512-AAAA".to_owned()));
        fs::remove_dir_all(&dir).map_err(|e| format!("{e}"))?;
        let (dir, snapshot) = fixture_snapshot("tampered", tarball, &"e".repeat(64))?;
        assert_eq!(verify_published(&snapshot, &"b".repeat(40), "@sidiora/layerx-sdk", "0.1.0").err().map(|r| r.code), Some(EXIT_PROVENANCE));
        fs::remove_dir_all(&dir).map_err(|e| format!("{e}"))
    }

    #[test]
    fn dependency_closure_gaps_and_links_are_refused() {
        let registry = "https://registry.npmjs.org";
        let gap = json!({"packages": {"": {"dependencies": {"a": "1"}}, "node_modules/a": {"version": "1", "resolved": "https://registry.npmjs.org/a/-/a-1.tgz", "integrity": "sha512-x", "dependencies": {"b": "1"}}}});
        assert_eq!(check_closure(&gap, registry).err().map(|r| r.code), Some(EXIT_PROVENANCE));
        let nested = json!({"packages": {"": {"dependencies": {"a": "1"}}, "node_modules/a": {"version": "1", "resolved": "https://registry.npmjs.org/a/-/a-1.tgz", "integrity": "sha512-x", "dependencies": {"b": "1"}}, "node_modules/a/node_modules/b": {"version": "1", "resolved": "https://registry.npmjs.org/b/-/b-1.tgz", "integrity": "sha512-y"}}});
        assert_eq!(check_closure(&nested, registry), Ok(()));
        let linked = json!({"packages": {"": {"dependencies": {"a": "1"}}, "node_modules/a": {"resolved": "../a", "link": true}}});
        assert_eq!(check_closure(&linked, registry).err().map(|r| r.code), Some(EXIT_PROVENANCE));
        let foreign = json!({"packages": {"": {}, "node_modules/a": {"version": "1", "resolved": "https://npm.example.invalid/a-1.tgz", "integrity": "sha512-x"}}});
        assert_eq!(check_closure(&foreign, registry).err().map(|r| r.code), Some(EXIT_PROVENANCE));
    }

    #[test]
    fn over_ten_artifact_records_failure() {
        let config = Config {
            repo_root: PathBuf::from("."),
            snapshot: PathBuf::from("s"),
            release_source_digest: "a".repeat(64),
            out: PathBuf::from("o"),
            registry: "https://registry.npmjs.org/".to_owned(),
        };
        let measured = [
            Measured { side: SIDES[0], sha256: "1".repeat(64), lines: 10, packages: Vec::new() },
            Measured { side: SIDES[1], sha256: "2".repeat(64), lines: 11, packages: Vec::new() },
        ];
        let passed = measured.iter().all(|m| m.lines <= THRESHOLD);
        let document = artifact(&config, &measured, passed);
        assert_eq!(document["passed"], json!(false));
        assert_eq!(document["threshold"], json!(10));
        assert_eq!(document["seller"]["lines"], json!(10));
        assert_eq!(document["buyer"]["lines"], json!(11));
    }
}
