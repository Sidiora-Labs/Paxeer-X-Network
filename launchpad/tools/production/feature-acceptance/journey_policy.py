from __future__ import annotations

import importlib.util
import pathlib
import re

_spec = importlib.util.spec_from_file_location('feature_acceptance_policy', pathlib.Path(__file__).with_name('policy.py'))
policy = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(policy)
Rejected = policy.Rejected
require = policy.require
keys = policy.keys
HEX = policy.HEX
ADDRESS = re.compile(r'0x[0-9a-fA-F]{40}\Z')
TX_HASH = re.compile(r'0x[0-9a-fA-F]{64}\Z')
QUANTITY = re.compile(r'0x(?:0|[1-9a-f][0-9a-f]*)\Z')
DECIMAL = re.compile(r'(?:0|[1-9][0-9]*)\Z')
TRANSFER = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef'
ECONOMIC = {'PF01', 'PF05', 'PF08', 'PF14', 'PF15', 'PF16', 'PF17', 'PF18', 'PF19', 'PF21', 'PF28', 'PF29', 'OC09', 'OC14'}
VOICE_FIELDS = {'listen', 'requestSpeaker', 'speak', 'moderate', 'end', 'providerQualified'}


def quantity(value):
    require(type(value) is str and len(value) <= 66 and QUANTITY.fullmatch(value), 'invalid chain quantity')
    return int(value, 16)


def amount(value):
    require(type(value) is str and len(value) <= 78 and DECIMAL.fullmatch(value), 'invalid raw amount')
    number = int(value)
    require(0 < number < 2**256, 'raw amount out of bounds')
    return number


def address(value):
    require(type(value) is str and ADDRESS.fullmatch(value), 'invalid chain address')
    return value.lower()


def tx_hash(value):
    require(type(value) is str and TX_HASH.fullmatch(value), 'invalid transaction hash')
    return value.lower()


def artifact_json(artifacts, path, kind):
    require(type(path) is str and path in artifacts and artifacts[path][0] == kind, 'missing typed journey artifact')
    value = policy.strict_json(artifacts[path][1])
    require(type(value) is dict, 'journey artifact must be object')
    return value


def receipt_pair(transaction, receipt):
    require(type(transaction) is dict and type(receipt) is dict, 'transaction and mined receipt required')
    transaction_hash = tx_hash(transaction.get('hash'))
    require(tx_hash(receipt.get('transactionHash')) == transaction_hash, 'receipt transaction mismatch')
    require(quantity(transaction.get('chainId')) == 125, 'settlement chain must be125')
    require(quantity(receipt.get('status')) == 1, 'settlement transaction failed')
    require(tx_hash(transaction.get('blockHash')) == tx_hash(receipt.get('blockHash')), 'settlement block mismatch')
    require(quantity(transaction.get('blockNumber')) == quantity(receipt.get('blockNumber')), 'settlement height mismatch')
    require(address(transaction.get('from')) == address(receipt.get('from')), 'settlement sender mismatch')
    require(address(transaction.get('to')) == address(receipt.get('to')), 'settlement destination mismatch')
    require(type(receipt.get('logs')) is list and len(receipt['logs']) <= 4096, 'receipt logs invalid')
    return transaction_hash


def transfer_in_receipt(transaction, receipt, transfer):
    keys(transfer, {'sender', 'recipient', 'asset', 'amount_raw'}, 'settlement transfer')
    sender, recipient = address(transfer['sender']), address(transfer['recipient'])
    expected = amount(transfer['amount_raw'])
    if transfer['asset'] is None:
        require(address(transaction['from']) == sender and address(transaction['to']) == recipient, 'native transfer parties mismatch')
        require(quantity(transaction.get('value')) == expected, 'native transfer amount mismatch')
        return
    token = address(transfer['asset'])
    matching = []
    for log in receipt['logs']:
        require(type(log) is dict and log.get('removed') is not True, 'removed or malformed settlement log')
        topics = log.get('topics')
        if type(topics) is not list or not topics or topics[0] != TRANSFER:
            continue
        if address(log.get('address')) != token:
            continue
        require(len(topics) == 3 and all(type(t) is str and re.fullmatch(r'0x[0-9a-fA-F]{64}', t) for t in topics), 'transfer topics invalid')
        require(all(t[2:26] == '0'*24 for t in topics[1:]), 'noncanonical transfer address topic')
        require(tx_hash(log.get('transactionHash')) == tx_hash(receipt['transactionHash']) and tx_hash(log.get('blockHash')) == tx_hash(receipt['blockHash']), 'transfer provenance mismatch')
        if topics[1][-40:].lower() == sender[2:] and topics[2][-40:].lower() == recipient[2:]:
            data = log.get('data')
            require(type(data) is str and re.fullmatch(r'0x[0-9a-fA-F]{64}', data), 'transfer data invalid')
            matching.append((quantity(log.get('logIndex')), int(data, 16)))
    require(len({i for i, _ in matching}) == len(matching), 'duplicate settlement log')
    require(len(matching) == 1 and matching[0][1] == expected, 'exact authorized transfer receipt missing')


def funding(record, context, authority):
    keys(record, {'type', 'provider_id', 'support_sha256', 'license_sha256', 'direction', 'rail', 'intent', 'transaction', 'receipt', 'transfer', 'registry'}, 'funding journey')
    require(record['type'] == 'provider_chain125' and record['registry'] == context['registry'], 'funding registry mismatch')
    provider = authority['providers'].get(record['provider_id'])
    require(type(provider) is dict and type(provider.get('chain_ids')) is list and all(type(c) is int for c in provider['chain_ids']) and 125 in provider['chain_ids'], 'provider chain125 support not admitted')
    require(all(type(record[k]) is str and HEX.fullmatch(record[k]) and record[k] == provider.get(k) for k in ['support_sha256', 'license_sha256']), 'provider support/licensing mismatch')
    require(record['direction'] in {'deposit', 'withdrawal'}, 'funding direction invalid')
    rail, intent = record['rail'], record['intent']
    require(type(rail) is dict and type(intent) is dict, 'real funding rail/intent required')
    require(rail.get('type') == 'provider_onramp' and rail.get('chainId') == '125' and rail.get('status') == 'supported' and rail.get('providerId') == record['provider_id'], 'unsupported funding rail')
    require(intent.get('railId') == rail.get('railId') and intent.get('state') == 'committed', 'funding not committed')
    require(intent.get('assetAddress') == rail.get('assetAddress') == record['transfer']['asset'], 'funding asset mismatch')
    require(amount(intent.get('receivedRaw')) == amount(intent.get('expectedRaw')) == amount(record['transfer']['amount_raw']), 'funding amount mismatch')
    require(tx_hash(intent.get('transactionHash')) == receipt_pair(record['transaction'], record['receipt']), 'funding receipt mismatch')
    wallet_party = 'recipient' if record['direction'] == 'deposit' else 'sender'
    require(address(intent.get('wallet')) == address(record['transfer'][wallet_party]), 'funding wallet mismatch')
    transfer_in_receipt(record['transaction'], record['receipt'], record['transfer'])


def settlement(record, feature, context, authority):
    keys(record, {'type', 'feature', 'registry', 'contract_address', 'code_sha256', 'action', 'transaction', 'receipt', 'transfers'}, 'economic settlement')
    require(record['type'] == 'settlement' and record['feature'] == feature and record['registry'] == context['registry'], 'settlement scope mismatch')
    admitted = authority['settlements'].get(feature)
    require(type(admitted) is list and any(type(a) is dict and a.get('contract_address') == record['contract_address'] and a.get('code_sha256') == record['code_sha256'] and a.get('action') == record['action'] for a in admitted), 'unsupported settlement authority')
    require(type(record['code_sha256']) is str and HEX.fullmatch(record['code_sha256']), 'settlement code digest invalid')
    receipt_pair(record['transaction'], record['receipt'])
    require(address(record['contract_address']) == address(record['transaction']['to']), 'settlement authority destination mismatch')
    require(type(record['transfers']) is list and 1 <= len(record['transfers']) <= 128, 'settlement transfers missing')
    require(len({policy.digest(t) for t in record['transfers']}) == len(record['transfers']), 'duplicate settlement transfer')
    for transfer in record['transfers']:
        transfer_in_receipt(record['transaction'], record['receipt'], transfer)


def native_device(record, context, authority, artifacts):
    keys(record, {'type', 'platform', 'physical', 'device_id', 'build_sha256', 'installed_build_sha256', 'source_sha256', 'application_id', 'push_provider', 'push_message_id', 'push_delivered_at', 'deep_link', 'resolved_route', 'auth_result_artifact', 'trade_result_artifact', 'trace_artifact'}, 'native device journey')
    require(record['type'] == 'native_device' and record['physical'] is True and record['platform'] in {'ios', 'android'}, 'physical native device required')
    expected = authority['native_builds'].get(record['platform'])
    require(type(expected) is dict and type(expected.get('device_ids')) is list and record['device_id'] in expected['device_ids'], 'native device not admitted')
    require(record['source_sha256'] == context['source'] and type(record['build_sha256']) is str and record['build_sha256'] == record['installed_build_sha256'] == expected.get('build_sha256') and HEX.fullmatch(record['build_sha256']), 'native build/candidate mismatch')
    require(record['application_id'] == expected.get('application_id'), 'native application mismatch')
    require(record['push_provider'] == ('apns' if record['platform'] == 'ios' else 'fcm'), 'native push platform mismatch')
    require(policy.text(record['push_message_id']) and policy.integer(record['push_delivered_at']), 'actual push delivery missing')
    require(context['issued_at'] <= record['push_delivered_at'] <= context['now'], 'push delivery outside current run')
    require(record['resolved_route'] == '/notifications' and type(record['deep_link']) is str and record['deep_link'] == expected.get('notification_deep_link'), 'push/deep-link route mismatch')
    artifact_json(artifacts, record['auth_result_artifact'], 'native_result')
    artifact_json(artifacts, record['trade_result_artifact'], 'native_result')
    require(record['trace_artifact'] in artifacts and artifacts[record['trace_artifact']][0] == 'trace', 'native trace missing')
    for key in ['auth_result_artifact', 'trade_result_artifact']:
        result = artifact_json(artifacts, record[key], 'native_result')
        require(result.get('candidate') == context['source'] and result.get('run_id') == context['run_id'] and type(result.get('tests')) is int and result['tests'] > 0 and all(type(result.get(k)) is int and result[k] == 0 for k in ['failed', 'skipped', 'exit_code']), 'nonpassing native auth/trade result')


def voice(record, authority):
    require(type(record) is dict and record.get('type') in {'voice_provider', 'voice_blocker'}, 'voice evidence type invalid')
    capabilities = record.get('capabilities')
    keys(capabilities, VOICE_FIELDS, 'VoiceCapabilities')
    require(all(type(v) is bool for v in capabilities.values()), 'voice capability boolean required')
    if record['type'] == 'voice_blocker':
        keys(record, {'type', 'capabilities', 'http_status', 'reason', 'owner', 'dependency', 'remediation', 'review_date', 'room_state', 'provider_session'}, 'voice blocker')
        require(not any(capabilities.values()) and record['http_status'] == 503 and record['room_state'] in {'scheduled', 'draft'} and record['provider_session'] is None, 'voice blocker grants capability')
        require(all(policy.text(record[k], 2048) for k in ['reason', 'owner', 'dependency', 'remediation', 'review_date']), 'voice blocker detail missing')
        return False
    keys(record, {'type', 'capabilities', 'provider_id', 'session_id', 'join_receipt', 'revocation_receipt', 'end_receipt'}, 'voice provider journey')
    require(capabilities['providerQualified'] is True and record['provider_id'] in authority['providers'], 'voice provider not admitted')
    require(all(policy.text(record[k], 2048) for k in ['session_id', 'join_receipt', 'revocation_receipt', 'end_receipt']), 'actual voice provider receipts missing')
    return True


def verify_journey(feature, envelope, evidence, *, context, trust, artifacts, authority, now):
    signer, _ = policy.verify_signature(envelope, trust, 'producer', feature)
    require(signer == evidence['signer'], 'journey producer mismatch')
    payload = envelope['payload']
    keys(payload, {'version', 'feature', 'evidence_sha256', 'records'}, 'journey envelope')
    require(payload['version'] == 2 and payload['feature'] == feature and payload['evidence_sha256'] == policy.digest(evidence), 'journey evidence binding mismatch')
    require(type(payload['records']) is list and 1 <= len(payload['records']) <= 128, 'journey records missing')
    require(len(set(payload['records'])) == len(payload['records']), 'duplicate journey artifact')
    keys(authority, {'providers', 'settlements', 'native_builds'}, 'trusted journey authority')
    require(all(type(v) is dict for v in authority.values()), 'trusted authority map invalid')
    records = []
    for path in payload['records']:
        require(path in artifacts and artifacts[path][0] in {'provider', 'receipt', 'device', 'state'}, 'journey record artifact missing')
        record = policy.strict_json(artifacts[path][1])
        require(type(record) is dict, 'journey record invalid')
        expected_kind = {'provider_chain125': 'provider', 'native_device': 'device', 'responsive_web': 'state', 'voice_provider': 'provider', 'voice_blocker': 'state', 'settlement': 'receipt'}.get(record.get('type'))
        require(expected_kind is not None and artifacts[path][0] == expected_kind, 'journey artifact type mismatch')
        if record['type'] == 'responsive_web':
            keys(record, {'type', 'viewport_width', 'browser', 'trace_artifact', 'state_artifact'}, 'responsive web journey')
            require(type(record['viewport_width']) is int and record['viewport_width'] in {390, 768, 1440} and policy.text(record['browser']), 'web viewport/browser invalid')
            require(record['trace_artifact'] in artifacts and artifacts[record['trace_artifact']][0] == 'trace' and record['state_artifact'] in artifacts and artifacts[record['state_artifact']][0] == 'state', 'actual web trace/state missing')
        records.append(record)
    require(all(type(record) is dict for record in records), 'journey record invalid')
    if feature == 'PF21':
        selected = [r for r in records if r.get('type') == 'provider_chain125']
        require({r.get('direction') for r in selected} == {'deposit', 'withdrawal'} and len(selected) == 2, 'actual vendor fund/withdraw proof required')
        for record in selected:
            funding(record, context, authority)
    if feature in {'PF24', 'OC23'}:
        selected = [r for r in records if r.get('type') == 'native_device']
        require(len(selected) == 2 and {r.get('platform') for r in selected} == {'ios', 'android'}, 'both actual native platforms required')
        for record in selected:
            native_device(record, {**context, 'issued_at': evidence['payload']['issued_at'], 'now': now}, authority, artifacts)
    if feature == 'PF27':
        selected = [r for r in records if r.get('type') in {'voice_provider', 'voice_blocker'}]
        require(len(selected) == 1 and voice(selected[0], authority), 'voice remains an explicit blocker')
    if feature in ECONOMIC - {'PF21'}:
        selected = [r for r in records if r.get('type') == 'settlement']
        require(selected, 'supported economic settlement proof required')
        for record in selected:
            settlement(record, feature, context, authority)
    return {'feature': feature, 'journey_policy_qualified': True, 'release_qualified': False}


def qualify_feature(feature, evidence, review, journey, *, inventory, context, trust, artifact_root, dependencies, authority, now):
    try:
        result = policy.qualify(feature, 'feature-user-path-v1', evidence, review, inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, now=now)
        artifacts = policy.verify_artifacts(evidence['payload']['artifacts'], artifact_root, evidence['payload']['issued_at'], now)
        verify_journey(feature, journey, evidence, context=context, trust=trust, artifacts=artifacts, authority=authority, now=now)
        return {**result, 'journey_policy_version': 2, 'release_qualified': False}
    except policy.Rejected:
        raise
    except (TypeError, KeyError, AttributeError, ValueError, OSError) as exc:
        raise policy.Rejected('malformed journey evidence or unavailable artifact') from exc


def qualify_audited_feature(feature, evidence, review, journey, audit, *, audit_authority, inventory, context, trust, artifact_root, dependencies, authority, now):
    try:
        result = qualify_feature(feature, evidence, review, journey, inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, authority=authority, now=now)
        artifacts = policy.verify_artifacts(evidence['payload']['artifacts'], artifact_root, evidence['payload']['issued_at'], now)
        audited = policy.verify_audit(feature, evidence, review, audit, context=context, trust=trust, artifacts=artifacts, audit_authority=audit_authority, now=now)
        challenges = policy.audit_adversarial(feature, evidence, review, required_traces=audit_authority['required_traces'], inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, now=now)
        return {**result, **audited, 'adversarial_checks': challenges['checks'], 'release_qualified': False}
    except policy.Rejected:
        raise
    except (TypeError, KeyError, AttributeError, ValueError, OSError) as exc:
        raise policy.Rejected('malformed audit evidence or unavailable artifact') from exc
