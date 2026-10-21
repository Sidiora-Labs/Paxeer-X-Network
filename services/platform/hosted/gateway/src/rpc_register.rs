use zeroize::Zeroizing;

const TOKEN_VARIABLE: &str = "LAYERX_GATEWAY_IDENTITY_PROVISIONING_TOKEN_FILE";

pub(super) fn configured_token() -> Result<Option<Zeroizing<String>>, String> {
    if std::env::var_os(TOKEN_VARIABLE).is_none() {
        return Ok(None);
    }
    super::read_secret(TOKEN_VARIABLE).map(Some)
}
