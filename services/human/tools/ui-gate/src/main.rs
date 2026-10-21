use std::env;
use std::fs;
use std::path::{Path, PathBuf};

const COMPONENT_FILES: &[&str] = &[
    "amount.tsx",
    "app-shell.tsx",
    "avatar.tsx",
    "badge.tsx",
    "balance-header.tsx",
    "bank-card.tsx",
    "button.tsx",
    "calendar-range-picker.tsx",
    "card.tsx",
    "code-entry.tsx",
    "code-input.tsx",
    "detail.tsx",
    "drawer.tsx",
    "empty-state.tsx",
    "feedback.tsx",
    "filters.tsx",
    "input.tsx",
    "list.tsx",
    "modal.tsx",
    "money-list.tsx",
    "notifications.tsx",
    "option-list.tsx",
    "popover.tsx",
    "primary-action.tsx",
    "quick-actions.tsx",
    "responsive-dialog.tsx",
    "search.tsx",
    "segmented-control.tsx",
    "sheet.tsx",
    "stat.tsx",
    "switch.tsx",
    "wizard.tsx",
];

const EXPORT_MARKERS: &[&str] = &[
    "./components/app-shell",
    "./components/button",
    "./components/card",
    "./components/input",
    "./components/list",
    "./components/modal",
    "./components/money-list",
    "./components/notifications",
    "./components/responsive-dialog",
    "./components/search",
    "./components/sheet",
    "./components/wizard",
];

const TOKEN_MARKERS: &[&str] = &[
    "--background:",
    "--surface:",
    "--surface-sunken:",
    "--foreground:",
    "--border:",
    "--border-strong:",
    "--primary:",
    "--accent:",
    "--success:",
    "--destructive:",
    "--warning:",
    "--radius-sheet:",
    "--shadow-card:",
    "--shadow-overlay:",
    "--font-sans:",
    "@theme inline",
];

const RAW_SCREEN_PATTERNS: &[(&str, &str)] = &[
    ("button", "Button, IconButton or PrimaryAction"),
    ("details", "DetailDisclosure"),
    ("dialog", "Modal, ResponsiveDialog or ConfirmDialog"),
    ("input", "Input, SearchInput or CodeInput"),
    ("select", "SegmentedControl, OptionList or FilterBar"),
];

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Rule {
    ApplicationWiring,
    CompetingPrimitive,
    ComponentInventory,
    DirectLibraryImport,
    PublicExports,
    RawScreenPattern,
    StyleContract,
    TokenContract,
}

impl Rule {
    const fn label(self) -> &'static str {
        match self {
            Self::ApplicationWiring => "application-wiring",
            Self::CompetingPrimitive => "competing-local-primitive",
            Self::ComponentInventory => "component-inventory",
            Self::DirectLibraryImport => "direct-layerx-ui-import",
            Self::PublicExports => "public-export",
            Self::RawScreenPattern => "raw-screen-pattern",
            Self::StyleContract => "style-contract",
            Self::TokenContract => "token-contract",
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct Violation {
    path: PathBuf,
    rule: Rule,
    detail: String,
}

fn read_required(path: &Path, rule: Rule, violations: &mut Vec<Violation>) -> Option<String> {
    match fs::read_to_string(path) {
        Ok(source) => Some(source),
        Err(error) => {
            violations.push(Violation {
                path: path.to_owned(),
                rule,
                detail: format!("required file is unavailable: {error}"),
            });
            None
        }
    }
}

fn require_markers(path: &Path, source: &str, markers: &[&str], rule: Rule) -> Vec<Violation> {
    markers
        .iter()
        .filter(|marker| !source.contains(**marker))
        .map(|marker| Violation {
            path: path.to_owned(),
            rule,
            detail: format!("missing {marker}"),
        })
        .collect()
}

fn is_competing_primitive(path: &Path) -> bool {
    let normalized = path.to_string_lossy().replace('\\', "/");
    normalized.contains("/src/components/ui/") || normalized.contains("/src/primitives/")
}

fn is_typescript_source(path: &Path) -> bool {
    path.extension()
        .is_some_and(|extension| extension == "ts" || extension == "tsx")
}

fn is_tsx_source(path: &Path) -> bool {
    path.extension().is_some_and(|extension| extension == "tsx")
}

fn is_kit_source(web_root: &Path, path: &Path) -> bool {
    path.strip_prefix(web_root.join("src/kit")).is_ok()
}

fn line_number(source: &str, offset: usize) -> usize {
    source[..offset].split('\n').count()
}

fn is_layerx_ui_specifier(specifier: &str) -> bool {
    specifier == "@layerx/ui" || specifier.starts_with("@layerx/ui/")
}

fn is_import_prefix(source: &str) -> bool {
    let compact = source
        .chars()
        .rev()
        .take(32)
        .collect::<String>()
        .chars()
        .rev()
        .filter(|character| !character.is_whitespace())
        .collect::<String>();
    compact.ends_with("from")
        || compact.ends_with("import")
        || compact.ends_with("import(")
        || compact.ends_with("require(")
}

fn direct_layerx_ui_import(source: &str) -> Option<usize> {
    let bytes = source.as_bytes();
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'/' && bytes.get(index + 1) == Some(&b'/') {
            index += 2;
            while index < bytes.len() && bytes[index] != b'\n' {
                index += 1;
            }
            continue;
        }
        if bytes[index] == b'/' && bytes.get(index + 1) == Some(&b'*') {
            index += 2;
            while index + 1 < bytes.len() && !(bytes[index] == b'*' && bytes[index + 1] == b'/') {
                index += 1;
            }
            index = (index + 2).min(bytes.len());
            continue;
        }
        if bytes[index] == b'`' {
            index += 1;
            let mut escaped = false;
            while index < bytes.len() {
                if escaped {
                    escaped = false;
                } else if bytes[index] == b'\\' {
                    escaped = true;
                } else if bytes[index] == b'`' {
                    index += 1;
                    break;
                }
                index += 1;
            }
            continue;
        }
        if bytes[index] != b'\'' && bytes[index] != b'"' {
            index += 1;
            continue;
        }

        let quote = bytes[index];
        let literal_offset = index;
        index += 1;
        let content_offset = index;
        let mut escaped = false;
        while index < bytes.len() {
            if escaped {
                escaped = false;
            } else if bytes[index] == b'\\' {
                escaped = true;
            } else if bytes[index] == quote {
                break;
            }
            index += 1;
        }
        if index >= bytes.len() {
            break;
        }
        let specifier = &source[content_offset..index];
        if is_layerx_ui_specifier(specifier)
            && is_import_prefix(source[..literal_offset].trim_end())
        {
            return Some(literal_offset);
        }
        index += 1;
    }
    None
}

fn mask_comments_and_strings(source: &str) -> Vec<u8> {
    let bytes = source.as_bytes();
    let mut masked = bytes.to_vec();
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'/' && bytes.get(index + 1) == Some(&b'/') {
            masked[index] = b' ';
            masked[index + 1] = b' ';
            index += 2;
            while index < bytes.len() && bytes[index] != b'\n' {
                masked[index] = b' ';
                index += 1;
            }
            continue;
        }
        if bytes[index] == b'/' && bytes.get(index + 1) == Some(&b'*') {
            masked[index] = b' ';
            masked[index + 1] = b' ';
            index += 2;
            while index < bytes.len() {
                if bytes[index] == b'*' && bytes.get(index + 1) == Some(&b'/') {
                    masked[index] = b' ';
                    masked[index + 1] = b' ';
                    index += 2;
                    break;
                }
                if bytes[index] != b'\n' {
                    masked[index] = b' ';
                }
                index += 1;
            }
            continue;
        }
        if !matches!(bytes[index], b'\'' | b'"' | b'`') {
            index += 1;
            continue;
        }

        let quote = bytes[index];
        masked[index] = b' ';
        index += 1;
        let mut escaped = false;
        while index < bytes.len() {
            let byte = bytes[index];
            if byte != b'\n' {
                masked[index] = b' ';
            }
            index += 1;
            if escaped {
                escaped = false;
            } else if byte == b'\\' {
                escaped = true;
            } else if byte == quote {
                break;
            }
        }
    }
    masked
}

fn raw_screen_patterns(source: &str) -> Vec<(&'static str, &'static str, usize)> {
    let masked = mask_comments_and_strings(source);
    let mut matches = Vec::new();
    for &(tag, component) in RAW_SCREEN_PATTERNS {
        let needle = format!("<{tag}");
        let mut offset = 0;
        while let Some(found) = masked[offset..]
            .windows(needle.len())
            .position(|window| window == needle.as_bytes())
        {
            let start = offset + found;
            let end = start + needle.len();
            if masked
                .get(end)
                .is_some_and(|byte| byte.is_ascii_whitespace() || matches!(*byte, b'>' | b'/'))
            {
                matches.push((tag, component, start));
            }
            offset = end;
        }
    }
    matches
}

fn visit_sources(root: &Path, files: &mut Vec<PathBuf>) -> Result<(), String> {
    if !root.exists() {
        return Ok(());
    }
    for entry in
        fs::read_dir(root).map_err(|error| format!("cannot read {}: {error}", root.display()))?
    {
        let path = entry.map_err(|error| error.to_string())?.path();
        if path.is_dir() {
            visit_sources(&path, files)?;
        } else {
            files.push(path);
        }
    }
    Ok(())
}

fn check_application_wiring(web_root: &Path, violations: &mut Vec<Violation>) {
    let checks: &[(&str, &[&str])] = &[
        (
            "package.json",
            &[
                "\"@layerx/ui\": \"file:packages/layerx-ui\"",
                "\"@tailwindcss/postcss\": \"4.3.3\"",
                "\"tailwindcss\": \"4.3.3\"",
            ],
        ),
        ("postcss.config.mjs", &["@tailwindcss/postcss"]),
        (
            "src/app/globals.css",
            &[
                "@import \"tailwindcss\"",
                "@import \"@layerx/ui/styles.css\"",
                "@source \"../../packages/layerx-ui/src\"",
            ],
        ),
        ("src/app/layout.tsx", &["./globals.css"]),
    ];
    for (relative, markers) in checks {
        let path = web_root.join(relative);
        if let Some(source) = read_required(&path, Rule::ApplicationWiring, violations) {
            violations.extend(require_markers(
                &path,
                &source,
                markers,
                Rule::ApplicationWiring,
            ));
        }
    }
}

fn check_package_contract(web_root: &Path, violations: &mut Vec<Violation>) {
    let package_root = web_root.join("packages/layerx-ui");
    let manifest = package_root.join("package.json");
    if let Some(source) = read_required(&manifest, Rule::ApplicationWiring, violations) {
        violations.extend(require_markers(
            &manifest,
            &source,
            &["\"name\": \"@layerx/ui\"", "\"version\": \"0.1.0\""],
            Rule::ApplicationWiring,
        ));
    }

    let index = package_root.join("src/index.ts");
    if let Some(source) = read_required(&index, Rule::PublicExports, violations) {
        violations.extend(require_markers(
            &index,
            &source,
            EXPORT_MARKERS,
            Rule::PublicExports,
        ));
    }

    let tokens = package_root.join("src/styles/tokens.css");
    if let Some(source) = read_required(&tokens, Rule::TokenContract, violations) {
        violations.extend(require_markers(
            &tokens,
            &source,
            TOKEN_MARKERS,
            Rule::TokenContract,
        ));
    }

    let components = package_root.join("src/components");
    for component in COMPONENT_FILES {
        let path = components.join(component);
        if !path.is_file() {
            violations.push(Violation {
                path,
                rule: Rule::ComponentInventory,
                detail: "required owner-supplied component is missing".to_owned(),
            });
        }
    }

    let style_checks: &[(&str, &[&str])] = &[
        ("app-shell.tsx", &["border-t border-border", "shadow-overlay"]),
        (
            "button.tsx",
            &["border border-border-strong", "focus-visible:ring-2"],
        ),
        ("card.tsx", &["border border-border shadow-card"]),
        ("bank-card.tsx", &["linear-gradient(135deg"]),
    ];
    for (relative, markers) in style_checks {
        let path = components.join(relative);
        if let Some(source) = read_required(&path, Rule::StyleContract, violations) {
            violations.extend(require_markers(
                &path,
                &source,
                markers,
                Rule::StyleContract,
            ));
        }
    }
}

fn check_existing_rules(web_root: &Path, violations: &mut Vec<Violation>) {
    check_application_wiring(web_root, violations);
    check_package_contract(web_root, violations);

    let mut application_sources = Vec::new();
    if let Err(error) = visit_sources(&web_root.join("src"), &mut application_sources) {
        violations.push(Violation {
            path: web_root.join("src"),
            rule: Rule::CompetingPrimitive,
            detail: error,
        });
    }
    for path in application_sources
        .into_iter()
        .filter(|path| is_competing_primitive(path))
    {
        violations.push(Violation {
            path,
            rule: Rule::CompetingPrimitive,
            detail: "use @layerx/ui instead of a parallel local primitive".to_owned(),
        });
    }
}

fn check_kit_composition(web_root: &Path, violations: &mut Vec<Violation>) {
    let source_root = web_root.join("src");
    let mut application_sources = Vec::new();
    if let Err(error) = visit_sources(&source_root, &mut application_sources) {
        violations.push(Violation {
            path: source_root,
            rule: Rule::RawScreenPattern,
            detail: error,
        });
        return;
    }

    for path in application_sources
        .into_iter()
        .filter(|path| is_typescript_source(path) && !is_kit_source(web_root, path))
    {
        let Some(source) = read_required(&path, Rule::RawScreenPattern, violations) else {
            continue;
        };
        if let Some(offset) = direct_layerx_ui_import(&source) {
            violations.push(Violation {
                path: path.clone(),
                rule: Rule::DirectLibraryImport,
                detail: format!(
                    "line {} imports @layerx/ui outside src/kit",
                    line_number(&source, offset)
                ),
            });
        }
        if is_tsx_source(&path) {
            for (tag, component, offset) in raw_screen_patterns(&source) {
                violations.push(Violation {
                    path: path.clone(),
                    rule: Rule::RawScreenPattern,
                    detail: format!(
                        "line {} uses raw <{tag}>; compose {component} through src/kit",
                        line_number(&source, offset)
                    ),
                });
            }
        }
    }
}

fn human_ui_rule_gates(web_root: &Path) -> Result<(), Vec<Violation>> {
    let mut violations = Vec::new();
    check_existing_rules(web_root, &mut violations);
    check_kit_composition(web_root, &mut violations);

    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

fn main() {
    let root = env::args()
        .nth(1)
        .unwrap_or_else(|| "apps/web".to_owned());
    match human_ui_rule_gates(Path::new(&root)) {
        Ok(()) => println!("LayerX UI component-library integrity gate passed"),
        Err(violations) => {
            for violation in violations {
                eprintln!(
                    "{}: {}: {}",
                    violation.path.display(),
                    violation.rule.label(),
                    violation.detail
                );
            }
            std::process::exit(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{
        check_existing_rules, check_kit_composition, is_competing_primitive, require_markers, Rule,
    };
    use std::error::Error;
    use std::fs;
    use std::path::Path;

    fn fixture(relative: &str) -> Result<(std::path::PathBuf, String), Box<dyn Error>> {
        let path = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("fixtures")
            .join(relative);
        let source = fs::read_to_string(&path)?;
        Ok((path, source))
    }

    #[test]
    fn missing_export_fixture_is_rejected() -> Result<(), Box<dyn Error>> {
        let (path, source) = fixture("missing-export.ts")?;
        let violations =
            require_markers(&path, &source, &["./components/card"], Rule::PublicExports);
        assert_eq!(violations.len(), 1);
        Ok(())
    }

    #[test]
    fn missing_token_fixture_is_rejected() -> Result<(), Box<dyn Error>> {
        let (path, source) = fixture("missing-token.css")?;
        let violations = require_markers(&path, &source, &["--shadow-card:"], Rule::TokenContract);
        assert_eq!(violations.len(), 1);
        Ok(())
    }

    #[test]
    fn competing_primitive_fixture_is_rejected() {
        let path =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("fixtures/src/components/ui/button.tsx");
        assert!(is_competing_primitive(&path));
    }

    #[test]
    fn raw_screen_composition_fixture_is_rejected() {
        let web_root = Path::new(env!("CARGO_MANIFEST_DIR")).join("fixtures/composition/raw");
        let mut violations = Vec::new();
        check_kit_composition(&web_root, &mut violations);
        assert!(violations
            .iter()
            .any(|violation| violation.rule == Rule::DirectLibraryImport));
        assert!(violations.iter().any(|violation| {
            violation.rule == Rule::RawScreenPattern && violation.detail.contains("raw <input>")
        }));
    }

    #[test]
    fn valid_kit_composition_fixture_passes() {
        let web_root = Path::new(env!("CARGO_MANIFEST_DIR")).join("fixtures/composition/valid");
        let mut violations = Vec::new();
        check_kit_composition(&web_root, &mut violations);
        assert!(violations.is_empty(), "{violations:?}");
    }

    #[test]
    fn checked_in_library_integration_preserves_existing_rules() {
        let web_root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../apps/web");
        let mut violations = Vec::new();
        check_existing_rules(&web_root, &mut violations);
        assert!(violations.is_empty(), "{violations:?}");
    }
}
