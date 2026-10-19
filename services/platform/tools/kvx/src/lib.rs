#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Entry {
    pub section: String,
    pub key: String,
    pub value: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Document {
    entries: Vec<Entry>,
}

impl Document {
    #[must_use]
    pub fn sections(&self) -> Vec<&str> {
        let mut names = Vec::new();
        for entry in &self.entries {
            if names.last() != Some(&entry.section.as_str()) {
                names.push(entry.section.as_str());
            }
        }
        names
    }

    #[must_use]
    pub fn get(&self, section: &str, key: &str) -> Option<&str> {
        self.entries
            .iter()
            .find(|entry| entry.section == section && entry.key == key)
            .map(|entry| entry.value.as_str())
    }

    #[must_use]
    pub fn section_entries(&self, section: &str) -> Vec<(&str, &str)> {
        self.entries
            .iter()
            .filter(|entry| entry.section == section)
            .map(|entry| (entry.key.as_str(), entry.value.as_str()))
            .collect()
    }

    /// Returns the section's value for the key.
    ///
    /// # Errors
    ///
    /// Names the missing declaration.
    pub fn required(&self, section: &str, key: &str) -> Result<&str, String> {
        self.get(section, key)
            .ok_or_else(|| format!("missing declaration {section}.{key}"))
    }
}

/// Parses a kvx document into ordered section entries.
///
/// # Errors
///
/// Refuses lines outside a section, non key/value lines and duplicate keys.
pub fn parse(source: &str) -> Result<Document, String> {
    let mut section = String::new();
    let mut seen_sections: Vec<String> = Vec::new();
    let mut entries: Vec<Entry> = Vec::new();
    for (number, raw) in source.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with("//") {
            continue;
        }
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|value| value.strip_suffix(']'))
        {
            if name.trim().is_empty() {
                return Err(format!("line {}: empty section name", number + 1));
            }
            if seen_sections.iter().any(|seen| seen == name) {
                return Err(format!("line {}: duplicate section {name}", number + 1));
            }
            seen_sections.push(name.to_owned());
            name.clone_into(&mut section);
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            return Err(format!(
                "line {} is not a key/value declaration",
                number + 1
            ));
        };
        if section.is_empty() {
            return Err(format!("line {} is outside a section", number + 1));
        }
        let key = key.trim();
        let key = if key.starts_with('"') {
            unquote(key).map_err(|error| format!("line {}: {error}", number + 1))?
        } else {
            key.to_owned()
        };
        if key.is_empty() {
            return Err(format!("line {}: empty key", number + 1));
        }
        if entries
            .iter()
            .any(|entry| entry.section == section && entry.key == key)
        {
            return Err(format!(
                "line {}: duplicate declaration {section}.{key}",
                number + 1
            ));
        }
        entries.push(Entry {
            section: section.clone(),
            key,
            value: value.trim().to_owned(),
        });
    }
    Ok(Document { entries })
}

/// Strips the surrounding quotes from a kvx string value.
///
/// # Errors
///
/// Refuses unquoted values.
pub fn unquote(value: &str) -> Result<String, String> {
    value
        .strip_prefix('"')
        .and_then(|inner| inner.strip_suffix('"'))
        .filter(|inner| !inner.contains('"'))
        .map(str::to_owned)
        .ok_or_else(|| format!("expected quoted value, got {value}"))
}

/// Splits a kvx list value into its quoted items.
///
/// # Errors
///
/// Refuses values that are not lists of quoted strings.
pub fn string_list(value: &str) -> Result<Vec<String>, String> {
    let inner = value
        .strip_prefix('[')
        .and_then(|inner| inner.strip_suffix(']'))
        .ok_or_else(|| format!("expected list, got {value}"))?;
    if inner.trim().is_empty() {
        return Ok(Vec::new());
    }
    inner.split(',').map(|item| unquote(item.trim())).collect()
}

/// Quotes a value for a kvx document.
#[must_use]
pub fn quote(value: &str) -> String {
    format!("\"{value}\"")
}
