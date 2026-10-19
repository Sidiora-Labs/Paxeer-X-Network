#![no_main]

mod support;

use layerx_agentd::store::{key, ObjectKind, TenantId};
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    let materialized = support::input_bytes(data);
    let data = materialized.as_ref();
    if data.len() < 2 {
        return;
    }
    let tenant_length = usize::from(data[0]).min(data.len() - 1).min(255);
    if tenant_length == 0 || data.len() <= tenant_length + 1 {
        return;
    }
    let Ok(tenant_text) = std::str::from_utf8(&data[1..1 + tenant_length]) else {
        return;
    };
    let Ok(tenant) = TenantId::new(tenant_text.to_owned()) else {
        return;
    };
    let object_id = &data[1 + tenant_length..];
    if object_id.is_empty() || object_id.len() > 4_096 {
        return;
    }
    let Ok(key) = key(tenant, ObjectKind::PreparedActivity, object_id.to_vec()) else {
        return;
    };
    let encoded = key.canonical_bytes();
    let encoded_tenant_length = usize::from(u16::from_be_bytes([encoded[0], encoded[1]]));
    assert_eq!(encoded_tenant_length, tenant_length);
    assert_eq!(&encoded[2..2 + tenant_length], tenant_text.as_bytes());
    assert_eq!(
        encoded[2 + tenant_length],
        ObjectKind::PreparedActivity as u8
    );
    let object_length_offset = 3 + tenant_length;
    let encoded_object_length = u32::from_be_bytes(
        encoded[object_length_offset..object_length_offset + 4]
            .try_into()
            .unwrap_or([0; 4]),
    ) as usize;
    assert_eq!(encoded_object_length, object_id.len());
    assert_eq!(&encoded[object_length_offset + 4..], object_id);
});
