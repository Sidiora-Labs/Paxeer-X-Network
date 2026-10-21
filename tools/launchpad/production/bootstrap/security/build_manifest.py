#!/usr/bin/env python3
"""Inventory existing source tests; never substitutes for executing them."""
import hashlib,json,pathlib,re
ROOT=pathlib.Path(__file__).resolve().parents[4]
GROUPS=[('wallet-and-webhook','shared',['auth']),('http-security','shared',['http']),('secret-provider','shared',['secrets']),('gateway-session','media/gateway',['internal/auth']),('gateway-upload','media/gateway',['internal/upload','internal/image']),('gateway-proxy','media/gateway',['internal/proxy']),('gateway-object-key','media/gateway',['internal/serve']),('api-rate-limit','core/api',['internal/ratelimit']),('stats-webhook','core/stats-workers',['internal/httpapi']),('pnl-webhook','core/pnl-tracker',['internal/httpapi']),('charts-webhook','core/trading-charts',['internal/httpapi']),('metadata-signature','media/metadata',['internal/metadata']),('user-signature','media/user',['internal/user'])]
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
