#!/usr/bin/env python3
"""Exact source-backed accounting validation; no RPC or receipt claims.

JSON Schema validates shape independently. This module validates arithmetic and
cross-field invariants which JSON Schema cannot establish. All money is integer
or Fraction; fixture addresses/hashes are explicit synthetic validation inputs.
"""
import argparse
import datetime
import fractions
import hashlib
import json
import math
import pathlib
import re
import sys
import tomllib

UINT256_MAX = 2**256 - 1
USDL = '0x85fcd13735f4309833a503ee804ea32395851479'
# Independent coverage manifest: removing fixture cases must not create green evidence.
REQUIRED_CASE_IDS = frozenset(('uint256_max', 'uint256_max_plus_one', 'uint256_79digits', 'uint256_leading_zero', 'uint256_exponent', 'uint256_negative', 'uint256_number', 'aggregate156', 'aggregate157', 'aggregate_above_uint256', 'price_component512', 'price_component513', 'price_half_usdl', 'price_512digit_numerator', 'price_513digit_numerator', 'price_zero_denominator', 'price_unreduced', 'price_unknown_decimals', 'price_spot_misbasis', 'pool_virtual_only', 'pool_effective_sum_wrong', 'pool_wrong_quote_decimals', 'token_metadata_unknown', 'token_unknown_disguised18', 'token_known_without_source', 'portfolio_known_cost', 'portfolio_partial_known_cost', 'portfolio_partial_zero_fabricated', 'portfolio_partial_realized_fabricated', 'portfolio_coverage_wrong', 'portfolio_balance_nonconserved', 'portfolio_wrong_unrealized', 'portfolio_current_complete_historical_partial', 'portfolio_unpriced', 'trade_optical_buy', 'trade_optical_sell', 'trade_sell_fee_wrongasset', 'trade_fee_rounding_wrong', 'trade_priced_above_paid', 'trade_exact_missing_input', 'candle_execution_valid', 'candle_partial_coverage_valid', 'candle_revision_zero', 'candle_synthetic_forbidden', 'candle_volume_mismatch', 'candle_week_thursday', 'ranking_valid', 'ranking_model_mismatch', 'ranking_nan_score', 'ranking_hidden_truncation', 'ranking_truncated_valid', 'portfolio_proportional_disposal', 'portfolio_unknown_proceeds_fabricated_gain', 'portfolio_final_disposal_consumes_basis', 'trade_full_reserves_buy', 'trade_buy_optical_surplus_misbooked', 'trade_full_reserves_sell', 'trade_sell_retained_fee_misbooked', 'trade_virtual_only_sell_rejected', 'portfolio_priced_missing_mark', 'portfolio_stale_missing_mark', 'portfolio_wrong_quote_currency'))


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def integer(value, digits=156, signed=False, positive=False, maximum=None):
    pattern = r'(0|-?[1-9][0-9]*)' if signed else r'(0|[1-9][0-9]*)'
    require(isinstance(value, str) and re.fullmatch(pattern, value) is not None, 'canonical integer decimal string required')
    require(len(value.lstrip('-')) <= digits, 'decimal digit bound exceeded')
    result = int(value)
    require(not positive or result > 0, 'positive quantity required')
    require(maximum is None or result <= maximum, 'uint256 maximum exceeded')
    return result


def ratio(value):
    n = integer(value['numerator'], 512)
    d = integer(value['denominator'], 512, positive=True)
    require(math.gcd(n, d) == 1, 'rational must be gcd-reduced')
    return fractions.Fraction(n, d)


def chain_order(position):
    require(position['chainId'] == '125', 'Paxeer chain125 required')
    return tuple(integer(position[k], maximum=UINT256_MAX) for k in ('height', 'transactionIndex', 'logIndex'))


def check_asset(asset):
    kind = asset['kind']
    if kind == 'native':
        require(asset['chainId'] == '125' and asset['address'] is None and asset['decimals'] == 18, 'native PAX identity/18decimals required')
    elif kind == 'erc20':
        require(asset['chainId'] == '125' and isinstance(asset['address'], str), 'ERC20 chain/address required')
    elif kind == 'fiat_display':
        require(asset['chainId'] is None and asset['address'] is None and bool(asset['currencyCode']), 'fiat provider currency identity required')
    else:
        raise ValueError('unknown asset kind')


def check_price(value):
    result = ratio(value)
    check_asset(value['baseAsset'])
    check_asset(value['quoteAsset'])
    require(value['baseAsset']['decimals'] is not None and value['quoteAsset']['decimals'] is not None, 'human price requires known decimals')
    source = value['source']
    if source['kind'] == 'external_display':
        require(source['position'] is None and bool(source['providerId']) and bool(source['sourceReference']), 'external display provider provenance required; no invented chain position')
        require(source['amountBasis'] == 'provider_display', 'external price amount basis mismatch')
    else:
        require(source['position'] is not None and bool(source['sourceEventId']), 'chain price source position/event required')
        chain_order(source['position'])
        basis = 'paid_input' if source['kind'] == 'gross_execution' else 'effective_reserves'
        require(source['amountBasis'] == basis, 'chain price amount basis mismatch')
    return result


def validate_semantics(schema, value, context=None):
    """Reject arithmetic/unit contradictions; structural validation is separate."""
    context = context or {}
    if schema == 'UInt256':
        integer(value, 78, maximum=UINT256_MAX)
    elif schema == 'AggregateUInt':
        integer(value)
    elif schema == 'PriceComponent':
        integer(value, 512)
    elif schema == 'Price':
        check_price(value)
    elif schema == 'Ratio':
        ratio(value)
    elif schema == 'Pool':
        virtual, real, effective = (integer(value[k], 78, maximum=UINT256_MAX) for k in ('virtualUsdlReserveRaw','realUsdlBalanceRaw','effectivePricingUsdlReserveRaw'))
        require(effective == virtual + real, 'effective reserve must equal virtual plus booked real USDL')
        require(value['quoteAssetAddress'].lower() == USDL and value['quoteAssetDecimals'] == 6, 'current pool quote is configured USDL6')
        require(value['chainId'] == '125', 'chain125 required')
    elif schema == 'Token':
        require((value['decimals'] is None) == (value['decimalsQuality'] == 'unavailable'), 'unknown decimals stay null with unavailable quality')
        require((value['totalSupplyRaw'] is None) == (value['supplyQuality'] == 'unavailable'), 'unknown supply stays null with unavailable quality')
        if value['decimalsQuality'] == 'verified' or value['supplyQuality'] == 'verified':
            require(value['sourcePosition'] is not None, 'verified chain metadata requires pinned source position')
    elif schema == 'PortfolioPosition':
        balance, known, unknown, cost = (integer(value[k]) for k in ('balanceRaw','knownQuantityRaw','unknownQuantityRaw','knownRemainingCostUsdlRaw'))
        require(balance == known + unknown, 'balance must equal known plus unknown basis quantities')
        require(known != 0 or cost == 0, 'zero known inventory has zero known remaining cost')
        expected_coverage = fractions.Fraction(known, balance) if balance else fractions.Fraction(0, 1)
        require(ratio(value['basisCoverage']) == expected_coverage, 'basisCoverage must represent exact reduced known quantity share')
        quality = 'complete' if unknown == 0 else ('unknown' if known == 0 else 'partial')
        require(value['basisQuality'] == quality, 'inventory basis quality disagrees with quantity coverage')
        require(value['remainingCostUsdlRaw'] == (str(cost) if unknown == 0 else None), 'complete basis equals known cost; unknown inventory requires null complete cost')
        if value['pricingQuality'] == 'unpriced':
            require(value['markPrice'] is None and value['unrealizedPnlRaw'] is None, 'unpriced inventory cannot invent a mark or unrealized profit')
        else:
            require(value['pricingQuality'] in ('priced','stale') and value['markPrice'] is not None, 'priced/stale position requires nonnull valid mark')
            mark = check_price(value['markPrice'])
            quote=value['markPrice']['quoteAsset']
            require(quote['kind']=='erc20' and quote['chainId']=='125' and quote['address'].lower()==USDL and quote['decimals']==6, 'USDL PnL requires configured USDL6 quote asset')
            if unknown == 0 and value['unrealizedPnlRaw'] is not None:
                decimals = value['markPrice']['baseAsset']['decimals']
                marked = mark * balance * 10**6 / 10**decimals
                expected_unrealized = marked.numerator // marked.denominator - cost
                require(integer(value['unrealizedPnlRaw'], signed=True) == expected_unrealized, 'unrealized PnL must use USDL6/token decimals and known remaining basis')
        require((value['realizedPnlRaw'] is None) == (value['realizedBasisQuality'] != 'complete'), 'complete realized PnL requires independent historical basis coverage')
        if value['realizedPnlRaw'] is not None:
            require(value['realizedPnlRaw'] == value['knownRealizedSubtotalUsdlRaw'], 'complete realized equals known subtotal')
        if unknown:
            require(value['unrealizedPnlRaw'] is None, 'partial basis cannot claim complete unrealized PnL')
        if 'disposal' in context:
            before=context['disposal']
            K,U,C,q,P,R=(integer(before[k],signed=(k=='knownRealizedSubtotalUsdlRaw')) for k in ('knownQuantityRaw','unknownQuantityRaw','knownRemainingCostUsdlRaw','disposeQuantityRaw','proceedsUsdlRaw','knownRealizedSubtotalUsdlRaw'))
            require(0 < q <= K+U, 'disposal cannot exceed inventory')
            k=q*K//(K+U)
            u=q-k
            pK=P*k//q
            c=C if k==K else (C*k//K if K else 0)
            require((known,unknown,cost)==(K-k,U-u,C-c), 'disposal conserves proportional known/unknown quantity and basis')
            require(integer(value['knownRealizedSubtotalUsdlRaw'],signed=True)==R+pK-c, 'known disposal proceeds minus removed known basis determines known realized subtotal')
        # Historical unknown disposals can keep realized null even after current
        # inventory becomes complete; do not infer historical basis completeness.
    elif schema == 'Trade':
        for k in ('tokenAmountRaw','usdlAmountRaw','feeRaw'):
            integer(value[k], 78, maximum=UINT256_MAX)
        require(integer(value['tokenAmountRaw']) > 0 and integer(value['usdlAmountRaw']) > 0, 'successful fill quantities are positive')
        buy = value['side'] == 'buy'
        expected_asset = USDL if buy else context['tokenAddress'].lower()
        require(value['feeAssetAddress'].lower() == expected_asset, 'fee is denominated in actual input asset')
        require(value['feeAssetDecimals'] == (6 if buy else context['tokenDecimals']), 'fee decimals must match input asset')
        require(value['feeDisposition'] == ('distributed_usdl' if buy else 'retained_token_reserve'), 'fee disposition must match actual buy/sell accounting')
        if value['paidInputRaw'] is not None:
            require(value['paidInputRaw'] == value['usdlAmountRaw' if buy else 'tokenAmountRaw'], 'gross consideration must equal emitted paid input')
        if value['inputDerivationQuality'] == 'exact':
            require(all(value[k] is not None for k in ('paidInputRaw','pricedInputRaw','ammInputAfterFeeRaw')), 'exact derivation requires nonnull input components')
            paid, priced, after, fee = (integer(value[k],78,maximum=UINT256_MAX) for k in ('paidInputRaw','pricedInputRaw','ammInputAfterFeeRaw','feeRaw'))
            require(0 < priced <= paid, 'optical priced input cannot exceed paid input or be zero')
            require(after == priced - fee, 'AMM input equals optical priced input minus fee')
            require(fee == priced * context['feeBps'] // 10000, 'fee must floor priced input times feeBps/10000')
        if 'reservesBefore' in context:
            before=context['reservesBefore']; after_reserves=context['reservesAfter']
            virtual,real,token=(integer(before[k],78,maximum=UINT256_MAX) for k in ('virtualUsdlReserveRaw','realUsdlBalanceRaw','tokenReserveRaw'))
            paid,after_fee,fee,output=(integer(value[k],78,maximum=UINT256_MAX) for k in ('paidInputRaw','ammInputAfterFeeRaw','feeRaw','tokenAmountRaw' if buy else 'usdlAmountRaw'))
            reserve_in,reserve_out=(virtual+real,token) if buy else (token,virtual+real)
            require(output==after_fee*reserve_out//(reserve_in+after_fee), 'output must match exact ReserveLib integer formula')
            require(buy or output<=real, 'virtual USDL cannot fund sell output')
            expected_real=real+paid-fee if buy else real-output
            expected_token=token-output if buy else token+paid
            require(int(after_reserves['realUsdlBalanceRaw'])==expected_real and int(after_reserves['tokenReserveRaw'])==expected_token, 'booked reserves must follow paid input and actual fee disposition')
        require((value['recipient'] is None) == (value['recipientAttribution'] == 'unavailable'), 'recipient nullability must agree with evidence quality')
    elif schema == 'Candle':
        require(value['synthetic'] is False, 'canonical candles exclude synthetic bars')
        integer(value['trades'], positive=True)
        integer(value['revision'], positive=True)
        prices = [check_price(value[k]) for k in ('open','high','low','close')]
        o,h,l,c = prices
        require(l <= o <= h and l <= c <= h, 'OHLC bounds must contain open and close')
        require(all(value[k]['source']['kind'] == 'gross_execution' for k in ('open','high','low','close')), 'canonical bars use gross execution prices, not marginal spot')
        require(chain_order(value['firstPosition']) <= chain_order(value['lastPosition']), 'candle first/last chain positions ordered')
        require(integer(value['volumeUsdlRaw']) == integer(value['buyVolumeUsdlRaw']) + integer(value['sellVolumeUsdlRaw']), 'total USDL volume equals buy plus sell volume')
        start = datetime.datetime.fromisoformat(value['start'].replace('Z','+00:00'))
        require(start.microsecond == 0, 'bucket boundary must have zero subseconds')
        periods = {'1m':60,'5m':300,'15m':900,'1h':3600,'4h':14400,'1d':86400}
        if value['interval'] == '1w':
            require(start.weekday() == 0 and start.hour == start.minute == start.second == 0, 'weekly candle starts Monday UTC')
        else:
            require(int(start.timestamp()) % periods[value['interval']] == 0, 'interval start must align to UTC bucket')
    elif schema == 'RankingSnapshot':
        require(value['replace'] is True, 'ranking update replaces immutable bounded view')
        require(len(value['items']) <= 100, 'ranking live view maximum100')
        addresses = set()
        for i,item in enumerate(value['items']):
            require(item['category'] == value['category'] and item['modelVersion'] == value['modelVersion'], 'ranking item category/model must match snapshot')
            require(item['rank'] == i+1, 'ranking ranks contiguous from1')
            require(item['poolAddress'].lower() not in addresses, 'ranking pools unique')
            addresses.add(item['poolAddress'].lower())
            require(re.fullmatch(r'-?(0|[1-9][0-9]*)(\.[0-9]+)?', item['score']) is not None, 'finite decimal ranking score required')
        total=integer(value['totalEligible'])
        require(total >= len(value['items']), 'eligible count cannot be below supplied entries')
        require(value['truncated'] == (total > len(value['items'])), 'truncation must disclose omitted eligible entries')
    else:
        raise ValueError('unsupported accounting schema '+schema)


def semantic_digest_for_spec(spec_path, catalog_path=None):
    # One shared convention with materialization; mutable task metadata is not
    # interface identity, but all normative catalog/rule changes remain pinned.
    from materialize import semantic_digest
    spec=json.loads(pathlib.Path(catalog_path).read_text()) if catalog_path else tomllib.loads(pathlib.Path(spec_path).read_text())
    if 'sections' in spec:spec=spec['sections']
    return semantic_digest(spec)


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--cases',required=True)
    parser.add_argument('--schema',required=True)
    parser.add_argument('--spec',required=True)
    parser.add_argument('--catalog')
    parser.add_argument('--major',type=int,default=1)
    args=parser.parse_args()
    import jsonschema
    data=json.loads(pathlib.Path(args.cases).read_text())
    actual_spec=hashlib.sha256(pathlib.Path(args.spec).read_bytes()).hexdigest()
    semantic_digest=semantic_digest_for_spec(args.spec,args.catalog)
    require(args.major>=1, "positive schema major required")
    if args.major!=1:require(data.get("schemaMajor")==args.major, "versioned accounting case provenance missing or mismatched")
    require(data.get('semanticInputSha256') == semantic_digest, 'fixture semantic interface/rules digest stale or absent')
    cases=data.get('cases')
    require(isinstance(cases,list) and bool(cases), 'nonempty accounting cases required')
    ids=[case.get('id') for case in cases]
    require(all(isinstance(id,str) and id for id in ids), 'case IDs must be nonempty strings')
    require(len(set(ids))==len(ids), 'duplicate accounting case IDs')
    require(REQUIRED_CASE_IDS <= set(ids), 'required accounting coverage missing: '+','.join(sorted(REQUIRED_CASE_IDS-set(ids))))
    defs=json.loads(pathlib.Path(args.schema).read_text())
    failures=[]
    for case in cases:
        for key in ('expectedSchemaValidity','expectedSemanticValidity','expectedValidity'):
            require(type(case.get(key)) is bool, 'expected validity must be boolean: '+case['id']+'.'+key)
        require(case['expectedValidity'] == (case['expectedSchemaValidity'] and case['expectedSemanticValidity']), 'declared combined validity inconsistent: '+case['id'])
        try:
            validate_semantics(case['schema'],case['instance'],case.get('context'))
            actual=True
        except (ValueError,KeyError,TypeError):
            actual=False
        if actual != case['expectedSemanticValidity']:
            failures.append(case['id']+':semantic expectation mismatch')
        validator=jsonschema.Draft202012Validator({'$ref':'#/$defs/'+case['schema'],'$defs':defs['$defs']},format_checker=jsonschema.FormatChecker())
        valid=not list(validator.iter_errors(case['instance']))
        if valid != case['expectedSchemaValidity']:
            failures.append(case['id']+':schema expectation mismatch')
        if case['expectedValidity'] != (valid and actual):
            failures.append(case['id']+':combined expectation mismatch')
    print(json.dumps({'status':'passed' if not failures else 'failed','caseCount':len(cases),'schemaChecked':True,'schemaMajor':args.major,'schemaSha256':hashlib.sha256(pathlib.Path(args.schema).read_bytes()).hexdigest(),'specSha256':actual_spec,'semanticInputSha256':semantic_digest,'failures':failures}))
    return 1 if failures else 0

if __name__=='__main__':
    try:
        sys.exit(main())
    except (ValueError,KeyError,TypeError,OSError,json.JSONDecodeError) as err:
        print(json.dumps({'status':'failed','error':str(err)}))
        sys.exit(1)
