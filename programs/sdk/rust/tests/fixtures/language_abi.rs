#![no_std]
use layerx_program_sdk as sdk;

sdk::trap_on_panic!();

fn publish(bytes: &[u8]) -> Result<i32, sdk::ProgramError> {
    sdk::call::publish_response(sdk::CallResult::new(7)?, bytes)?;
    Ok(7)
}

fn handle(input: &[u8]) -> Result<i32, sdk::ProgramError> {
    let invalid = || sdk::ProgramError::Host(sdk::HostRefusal::Invalid);
    let operation = *input.first().ok_or_else(invalid)?;
    if input.len() != if operation == 5 { 33 } else { 1 } {
        return Err(invalid());
    }
    match operation {
        0 => {
            sdk::storage::write(
                sdk::StorageKey::new(b"key")?,
                sdk::StorageValue::new(b"binding")?,
            )?;
            sdk::event::emit(
                sdk::EventTopic::new(b"topic")?,
                sdk::EventData::new(b"binding")?,
            )?;
            publish(b"binding")
        }
        1 => {
            let observation = sdk::oracle::read(&[0x11; 32])?;
            let mut record = [0; 64];
            record[..16].copy_from_slice(&observation.price.to_le_bytes());
            record[16..24].copy_from_slice(&observation.observed_at.to_le_bytes());
            record[24..32].copy_from_slice(&observation.sequence.to_le_bytes());
            record[32..].copy_from_slice(&observation.source_set_digest);
            publish(&record)
        }
        2 => {
            let mut record = [0; sdk::RECORD_BYTES];
            let length = {
                let answer =
                    sdk::web::read(0x0102_0304_0506_0708, &mut record)?.ok_or_else(invalid)?;
                sdk::ANSWER_HEADER_BYTES + answer.response.len()
            };
            publish(&record[..length])
        }
        3 => {
            sdk::call::publish_refusal(sdk::ProgramRefusal::new(
                sdk::RefusalClass::Rejected,
                sdk::RefusalReason::new(b"no")?,
            )?)?;
            Ok(sdk::CANDIDATE_REFUSAL_SENTINEL)
        }
        4 => {
            let mut output = [0; 64];
            let response = sdk::call::invoke_response(
                sdk::ProgramId::new([0x72; 32])?,
                sdk::CallInput::new(&[6])?,
                sdk::GrantedCapabilities::new(&[0, 0])?,
                &mut output,
            )?;
            let code = response.code();
            sdk::call::publish_response(
                sdk::CallResult::new(u32::try_from(code).map_err(|_| invalid())?)?,
                response.bytes(),
            )?;
            Ok(code)
        }
        5 => {
            let mut destination = [0; 32];
            destination.copy_from_slice(&input[1..]);
            let mut asset = [0; 32];
            asset[0] = 9;
            sdk::transfer::fund_program_account(sdk::ProgramDeposit::new(
                sdk::ProgramAccountSeed::new(b"fixture")?,
                sdk::AccountId::new(destination)?,
                sdk::AssetId::new(asset)?,
                sdk::Amount::from_u128(1),
            )?)?;
            publish(b"binding")
        }
        6 => publish(b"binding"),
        7 => {
            sdk::storage::write(
                sdk::StorageKey::new(b"key")?,
                sdk::StorageValue::new(b"binding")?,
            )?;
            publish(b"binding")
        }
        _ => Err(invalid()),
    }
}

#[no_mangle]
pub extern "C" fn layerx_reserve(length: i32) -> i32 {
    sdk::entry::reserve_call_input(length)
}

#[no_mangle]
pub extern "C" fn layerx_call(pointer: i32, length: i32) -> i32 {
    match sdk::entry::with_call_input(pointer, length, handle) {
        Ok(Ok(code)) => code,
        Ok(Err(error)) | Err(error) => error.code(),
    }
}
