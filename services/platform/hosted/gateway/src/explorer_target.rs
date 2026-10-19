pub const PREFIX: &str = "/explorer/backend";
pub const MAX_TARGET: usize = 4096;

pub fn owns_target(target: &str) -> bool {
    target
        .strip_prefix(PREFIX)
        .is_some_and(|rest| rest.is_empty() || rest.starts_with('/') || rest.starts_with('?'))
}

pub fn split_target(target: &str) -> Result<(&str, Option<&str>), String> {
    if target.len() > MAX_TARGET {
        return Err("explorer target exceeds bound".into());
    }
    let (path, query) = target
        .split_once('?')
        .map_or((target, None), |(p, q)| (p, Some(q)));
    if !path.starts_with('/')
        || path.len() > 2048
        || path.contains(['#', '\\', '%'])
        || !path.bytes().all(|b| b.is_ascii_graphic())
        || path.split('/').any(|part| matches!(part, "." | ".."))
        || path.contains("//")
    {
        return Err("explorer path refused".into());
    }
    if let Some(query) = query {
        if query.len() > 2048 {
            return Err("explorer query exceeds bound".into());
        }
        let bytes = query.as_bytes();
        let mut offset = 0;
        while offset < bytes.len() {
            if bytes[offset] == b'%' {
                if !bytes
                    .get(offset + 1..offset + 3)
                    .is_some_and(|v| v.iter().all(u8::is_ascii_hexdigit))
                {
                    return Err("explorer query escape refused".into());
                }
                offset += 3;
            } else if bytes[offset].is_ascii_alphanumeric()
                || b"-._~!$&'()*+,;=:@/?[]".contains(&bytes[offset])
            {
                offset += 1;
            } else {
                return Err("explorer query character refused".into());
            }
        }
    }
    Ok((path, query))
}

