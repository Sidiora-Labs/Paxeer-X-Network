#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

sys.dont_write_bytecode = True
definition = importlib.util.spec_from_file_location(
    'interface_artifact_support', Path(__file__).with_name('program-interface-producer.py'))
support = importlib.util.module_from_spec(definition)
definition.loader.exec_module(support)
ROOT = support.ROOT
require = support.require
SCHEMA = 'paxeer-x.program-storage-artifacts.v1'
TARGETS = ['abi_linker', 'composition', 'isolation', 'namespace_drop', 'shared_storage', 'storage_scan']
SUITES = {'abi_linker': {'binary': 'abi_linker', 'filter': None, 'required': ['every_frozen_import_instantiates_against_its_revision_linker', 'wrong_signatures_duplicates_and_extra_imports_are_rejected']}, 'composition': {'binary': 'composition', 'filter': None, 'required': ['authority_denial_does_not_create_a_phantom_edge_or_start_the_child', 'delegated_capability_escalation_matrix_never_enters_the_child', 'direct_and_indirect_reentrancy_are_typed_and_atomic', 'inherited_program_spend_narrows_across_depth_fanout_and_repeated_visits', 'nested_guest_event_aggregate_accepts_sixty_four_and_rolls_back_sixty_five', 'production_depth_boundary_and_one_past_are_atomic', 'production_edge_boundary_and_one_past_are_independently_typed', 'production_fanout_boundary_and_one_past_are_atomic', 'production_visit_boundary_and_one_past_are_atomic']}, 'isolation': {'binary': 'isolation', 'filter': None, 'required': ['abi_v1_manifest_matches_typed_declarations_and_golden', 'abi_v2_manifest_matches_typed_declarations_and_golden', 'candidate_linker_uses_the_same_bounded_guest_memory_boundary', 'capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects', 'denied_event_and_transfer_guests_have_no_effects', 'denied_guest_storage_write_is_stable_and_has_no_effect', 'exact_seven_function_surface_validates_and_instantiates', 'guest_memory_bounds_refusal_cannot_write_or_emit_effects', 'host_table_contains_seven_unique_names', 'namespace_persistence_bridge_preserves_all_scopes_and_exact_reclamation', 'nested_frames_use_their_own_program_memory_and_storage_namespace', 'principal_and_shared_namespaces_are_closed_ordered_and_disjoint', 'program_spend_capability_binds_owner_seed_account_asset_and_destination', 'protocol_private_namespace_is_unreachable_from_guest_selectors_and_keys', 'receipt_and_balance_sight_fail_closed_without_verified_authority', 'real_guest_storage_is_scoped_by_program_and_principal', 'state_gauntlet::shared_state_attacks_are_defeated_by_construction', 'unknown_and_ambient_kernel_imports_are_rejected', 'wrong_kind_is_rejected_during_validation', 'wrong_signature_is_rejected_during_validation']}, 'namespace_drop': {'binary': 'namespace_drop', 'filter': None, 'required': ['candidate_denied_and_invalid_selectors_do_not_reclaim_or_meter', 'candidate_drop_meter_distinguishes_cells_and_key_value_bytes', 'candidate_drop_meter_is_exact_and_one_past_refuses_before_mutation', 'candidate_drop_of_empty_namespace_records_zero_provisional_reclamation_fact', 'candidate_drop_preserves_every_adjacent_program_principal_and_scope', 'candidate_drop_reclaims_every_cell_in_the_sixty_four_cell_boundary_fixture', 'candidate_drop_then_write_and_write_then_drop_have_deterministic_ordering', 'candidate_later_fault_discards_namespace_drop_and_provisional_reclamation_fact', 'candidate_later_typed_host_refusal_discards_namespace_drop_and_effects']}, 'shared_storage': {'binary': 'shared_storage', 'filter': None, 'required': ['candidate_guest_increments_one_shared_total_for_two_principals', 'candidate_guest_shared_read_only_cannot_mutate_the_total', 'candidate_guest_shared_read_succeeds_and_delete_is_denied_without_mutation', 'candidate_program_call_narrows_shared_authority_before_child_entry', 'declared_access_executes_calldata_selected_guest_keys_and_charges_excess', 'declared_access_is_enforced_inside_real_nested_guest_frame', 'declared_access_rolls_back_prior_state_selected_guest_write', 'invalid_guest_selectors_refuse_before_memory_or_storage_access', 'principal_and_shared_access_charge_identical_bytes', 'principal_and_shared_grants_do_not_cross_and_denials_are_unmetered', 'selector_values_are_frozen_and_invalid_values_are_typed', 'shared_capabilities_encode_append_only_and_narrow_downward', 'two_principals_update_one_shared_total_with_equal_metering']}, 'storage_scan': {'binary': 'storage_scan', 'filter': None, 'required': ['candidate_scan_enforces_complete_page_byte_ceiling_independently_of_entry_ceiling', 'candidate_scan_host_returns_empty_and_single_empty_prefix_pages', 'candidate_scan_observes_same_activity_writes_and_a_later_failure_rolls_them_back', 'candidate_scan_paginates_across_activities_and_is_insertion_order_independent', 'candidate_scan_refusals_are_unmetered_and_leave_output_sentinel_unchanged', 'candidate_scan_rejects_cross_scope_prefix_and_limit_cursor_reuse', 'candidate_scan_status_fixtures_preserve_negative_status_and_scan_output_sentinel']}, 'abi_units': {'binary': 'layerx_programs_runtime', 'filter': 'abi::', 'required': ['abi::codec::tests::bytes_encoding_is_canonical', 'abi::codec::tests::decoded_size_limit_is_enforced', 'abi::codec::tests::empty_bytes_is_canonical', 'abi::codec::tests::empty_calldata_is_valid', 'abi::codec::tests::evm_convention_tag_roundtrips', 'abi::codec::tests::evm_head_only_requires_32_byte_alignment', 'abi::codec::tests::fixed_array_nesting_is_bounded', 'abi::codec::tests::input_size_limit_is_enforced', 'abi::codec::tests::integer_encodings_are_canonical', 'abi::codec::tests::invalid_convention_tag_is_rejected', 'abi::codec::tests::invalid_option_discriminator_is_rejected', 'abi::codec::tests::layerx_convention_tag_roundtrips', 'abi::codec::tests::option_none_is_canonical', 'abi::codec::tests::option_some_is_canonical', 'abi::codec::tests::union_encoding_is_canonical', 'abi::context::tests::frozen_field_ids_and_encodings_are_canonical', 'abi::context::tests::zero_protocol_fields_never_authenticate', 'abi::event_tests::event_bytes_are_charged_before_staging_and_exhaust_atomically', 'abi::event_tests::event_count_boundary_refuses_before_a_sixty_fifth_stage', 'abi::event_tests::maximum_largest_grant_set_fits_the_transport_ceiling']}, 'storage_units': {'binary': 'layerx_programs_runtime', 'filter': 'storage::', 'required': ['storage::ordered_scan::tests::ceiling_paginates_in_key_order_independent_of_insertion_order', 'storage::ordered_scan::tests::empty_prefix_and_single_entry_return_canonical_entry_without_cursor', 'storage::ordered_scan::tests::entry_and_complete_page_byte_ceilings_have_independent_exact_bounds', 'storage::ordered_scan::tests::foreign_cursor_and_nonfitting_entry_are_refused', 'storage::ordered_scan::tests::namespace_ranges_preserve_prefix_pages_and_metering_with_foreign_state', 'storage::ordered_scan::tests::principal_and_shared_scans_require_their_distinct_read_grants', 'storage::ordered_scan::tests::scan_requires_matching_read_authority_meters_full_pages_and_resumes_across_activities']}}


def command():
    argv = ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path',
            str(ROOT / 'programs/Cargo.toml'), '-p', 'layerx-programs-runtime', '--lib']
    for target in TARGETS:
        argv.extend(['--test', target])
    return argv + ['--no-run', '--message-format=json']


def run(argv, log, environment, deadline):
    remaining = deadline - time.monotonic()
    require(remaining > 0, 'original 30-minute time limit exhausted')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('COMMAND ' + json.dumps(argv) + '\nCWD ' + str(ROOT / 'programs') + '\n').encode())
        stream.flush()
        try:
            code = subprocess.run(argv, cwd=ROOT / 'programs', env=environment,
                                  stdin=subprocess.DEVNULL, stdout=stream,
                                  stderr=subprocess.STDOUT, timeout=remaining).returncode
        except subprocess.TimeoutExpired:
            code = 124
        except OSError as error:
            stream.write(str(error).encode())
            code = 127
        stream.write(f'\nEXIT {code}\n'.encode())
        stream.flush()
        os.fsync(stream.fileno())
    print(f'COMMAND_EXIT {code} LOG {log}', flush=True)
    require(code == 0, f'command exited {code}; log={log}')
    return support.artifact(log)


def executables(log):
    expected = set(TARGETS) | {'layerx_programs_runtime'}
    found = {}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if (event.get('reason') == 'compiler-artifact'
                and event.get('profile', {}).get('test') is True
                and event.get('executable')):
            name = event.get('target', {}).get('name')
            if name in expected:
                require(name not in found, 'duplicate runtime test executable')
                require(not event.get('features'), 'unexpected runtime features')
                found[name] = Path(event['executable'])
    require(finished and set(found) == expected, 'incomplete successful runtime build')
    return found


def build(output):
    deadline = time.monotonic() + 1800
    source = support.identity()
    directory = support.private_directory(output, create=True)
    require(not any(directory.iterdir()), 'build output must be fresh and empty')
    jobs = int(os.environ.get('PAXEER_X_STORAGE_BUILD_JOBS', '4'))
    require(1 <= jobs <= 16, 'build jobs must be between 1 and 16')
    environment = dict(os.environ, CARGO_TARGET_DIR=str(directory / 'cargo-target'),
                       CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
                       CARGO_INCREMENTAL='0', CARGO_BUILD_JOBS=str(jobs),
                       PYTHONDONTWRITEBYTECODE='1')
    record = {'schema': SCHEMA, 'source': source, 'producer_root': str(ROOT),
              'command': command(), 'cwd': str(ROOT / 'programs'),
              'profiles': {'dev_debug': 0, 'test_debug': 0, 'incremental': False},
              'features': [], 'suites': SUITES,
              'toolchains': {'cargo': support.capture(['cargo', '+1.91.1', '--version']),
                             'rustc': support.capture(['rustc', '+1.91.1', '-vV'])}}
    support.write_private(directory / 'build-inputs.json', record)
    log = directory / 'build.log'
    record['build_log'] = run(command(), log, environment, deadline)
    binaries = executables(log)
    record['artifacts'] = {}
    for name, binary in sorted(binaries.items()):
        destination = directory / name
        shutil.copyfile(binary, destination)
        destination.chmod(0o700)
        record['artifacts'][name] = support.artifact(destination)
    require(support.identity() == source, 'source changed during build')
    support.write_private(directory / 'manifest.json', record)
    print('STORAGE_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        build(args.output)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'storage build refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
