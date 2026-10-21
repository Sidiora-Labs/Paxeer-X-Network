#!/usr/bin/env python3
"""Inventory existing source tests; never substitutes for executing them."""
import hashlib,json,pathlib,re
ROOT=pathlib.Path(__file__).resolve().parents[5]
GROUPS=[('wallet-and-webhook','sdk/launchpad/shared',['auth']),('http-security','sdk/launchpad/shared',['http']),('secret-provider','sdk/launchpad/shared',['secrets']),('gateway-session','services/launchpad/gateway',['internal/auth']),('gateway-upload','services/launchpad/gateway',['internal/upload','internal/image']),('gateway-proxy','services/launchpad/gateway',['internal/proxy']),('gateway-object-key','services/launchpad/gateway',['internal/serve']),('api-rate-limit','services/launchpad/api',['internal/ratelimit']),('stats-webhook','services/launchpad/stats-workers',['internal/httpapi']),('pnl-webhook','services/launchpad/pnl-tracker',['internal/httpapi']),('charts-webhook','services/launchpad/trading-charts',['internal/httpapi']),('metadata-signature','services/launchpad/metadata',['internal/metadata']),('user-signature','services/launchpad/user',['internal/user'])]
rows=[]
for ident,module,packages in GROUPS:
    sources=[]
    for package in packages:
        for f in sorted((ROOT/module/package).glob('*_test.go')):
            if 'webhook' in ident and f.name!='webhook_test.go':continue
            content=f.read_text()
            sources.append({'path':str(f.relative_to(ROOT)),'sha256':hashlib.sha256(f.read_bytes()).hexdigest(),'tests':re.findall(r'^func (Test\w+)\(t \*testing.T\)',content,re.M)})
    selector=' -run ^TestWebhook' if 'webhook' in ident else ''
    rows.append({'id':ident,'cwd':module,'command':'GOWORK=off GOMAXPROCS=2 go test -p 1 -count=1 -json '+ ' '.join('./'+p for p in packages)+selector,'sources':sources,'execution_status':'not_executed_by_inventory','requires':['Go toolchain and resolved module dependencies'] + (['Docker daemon and redis:7-alpine image for Redis-backed cases'] if ident in ['http-security','gateway-session'] else []) + (['Package internal fixture dependencies; inspect package helper before fixture execution'] if ident in ['metadata-signature','user-signature'] else []),'skip_policy':'Any skip, zero executed test cases, build failure or nonzero exit fails qualification.'})
print(json.dumps({'schema_version':1,'scope':'Existing security-related regression inventory; not a production security certificate','checks':rows},indent=2))
