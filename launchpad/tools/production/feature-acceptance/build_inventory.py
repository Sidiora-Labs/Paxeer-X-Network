"""Reproducibly materialize declaration-only inventory from frozen specification."""
import json
import pathlib
import tomllib
ROOT = pathlib.Path(__file__).resolve().parents[3]
SPEC = ROOT/'spec/production-system/spec.kvx'
# Explicit behavioral owners, not aggregate domain-coverage inference.
OWNERS = {
'PF01':[27,39,41], 'PF02':[27,20], 'PF03':[19,20,30], 'PF04':[21,37,38],
'PF05':[26,3], 'PF06':[10,11,12,13,30], 'PF07':[26,28,29,30], 'PF08':[28,47],
'PF09':[35,31], 'PF10':[21,32,35], 'PF11':[14,16,17,31], 'PF12':[19,21],
'PF13':[33,35,37], 'PF14':[18,31], 'PF15':[18,27,3], 'PF16':[18,3],
'PF17':[18,31,3], 'PF18':[26,3], 'PF19':[28,3], 'PF20':[25,33,34,32],
'PF21':[25,32], 'PF22':[25,35], 'PF23':[33,35,39], 'PF24':[32,25],
'PF25':[40,30], 'PF26':[40,41], 'PF27':[36,38], 'PF28':[47,3],
'PF29':[47,34], 'PF30':[38,47,43],
'OC01':[4,5,6], 'OC02':[2,5], 'OC03':[1,4,6], 'OC04':[2,3],
'OC05':[10,13,14,18], 'OC06':[10,11,12], 'OC07':[8,12,23,24],
'OC08':[22,23,24], 'OC09':[25,26], 'OC10':[33,34,35], 'OC11':[39,41],
'OC12':[15,19,20], 'OC13':[14,16,17], 'OC14':[18,3], 'OC15':[38,47],
'OC16':[6,8], 'OC17':[7,9,42], 'OC18':[43,48], 'OC19':[44,49],
'OC20':[1,4], 'OC21':[34,45,46], 'OC22':[40,41,42,48],
'OC23':[29,30,31,32,49], 'OC24':[3,25,32,38,40,47], 'OC25':[49,50],
}
SPECIAL = {
 'PF08':[('pecor_executor','contract','L28','Accept executor deployment and exact supported order semantics')],
 'PF10':[('push_email','vendor','L21','Verify actual provider delivery/revocation and platform credentials'),('native_devices','device','L32','Run signed iOS and Android device journeys')],
 'PF14':[('referral_settlement','contract','L18','Prove authorized funded settlement and reconcile actual receipts')],
 'PF15':[('creator_authority','contract','L18','Verify current ownership and allowed fee-sharing/CTO transitions')],
 'PF16':[('legacy_cashback','contract','L18','Provide explicitly supported funded legacy cashback authority')],
 'PF17':[('reward_escrow','contract','L18','Prove authorized funding/liability and payment reconciliation')],
 'PF21':[('funding_chain125','vendor','L25','Establish provider chain125 support and real funding/withdrawal receipts')],
 'PF24':[('native_devices','device','L32','Install signed candidates on physical iOS and Android and verify push/deep links')],
 'PF26':[('broadcast_provider','vendor','L40','Prove real ingest/CDN playback, licensing, credentials and reconnect')],
 'PF27':[('voice_provider','vendor','L38','Verify managed voice membership, moderation and revocation')],
 'PF28':[('bounty_escrow','contract','L47','Deploy/accept authorized funded escrow and dispute/settlement semantics')],
 'PF29':[('automation_executor','contract','L47','Verify executor permissions, disclosed bounds and kill switch')],
 'OC14':[('reward_escrow','contract','L18','Reconcile real authorized liabilities and funded receipts')],
 'OC19':[('alternate_region','service','L44','Provision alternate topology and prove timed real restore')],
 'OC22':[('independent_load_fleet','service','L48','Calibrate four external generators and perform full soak/surge with cost evidence')],
 'OC23':[('native_devices','device','L32','Physical signed iOS/Android accessibility and wallet journeys')],
 'OC24':[('vendor_licensing','vendor','L47','Accept signed chain125 support and licensing evidence for every enabled vendor')],
}
INHERITED = {
'C07':('contract','L26','Verify codehash-specific optical hook quote adapters and wallet-context simulation; unknown optical quotes stay unavailable'),
'C03':('contract','L28','Accept current PECOR executor and conditional order authority'),
'C04':('contract','L18','Verify authorized funded cashback/reward settlement'),
'C02':('contract','L28','Accept custom pair/route contract support'),
'C05':('contract','L47','Accept funded bounty escrow and dispute settlement'),
'C06':('contract','L47','Accept bounded automation executor authority'),
'D03':('service','L25','Verify regular external Web3 wallet signing/linking without hosted custody; legacy embedded provider is superseded'),
'D04':('vendor','L25','Verify real chain125 funding/withdrawal provider support'),
'D05':('vendor','L40','Verify live media provider regions, retention/cost, voice semantics and staffed moderation together with L38'),
}
CONTRACT = {'PF01','PF02','PF05','PF06','PF07','PF11','PF15','PF18','PF19','PF20','PF22','OC01','OC02','OC03','OC04','OC05','OC06','OC09','OC13','OC20'}
def build():
 s=tomllib.loads(SPEC.read_text()); features=[]
 refs={r['legacy_feature_id']:(k,r) for k,r in s['req'].items() if r.get('legacy_feature_id')}
 for fid in sorted(OWNERS):
  rid, req=refs[fid]; coverage=s['coverage'][fid]
  deps=[{'id':fid+':integrated_services','kind':'service','owner':f'L{OWNERS[fid][0]:02}', 'remediation':'Wire actual owned services and execute this feature end to end against the current candidate'}]
  if fid in CONTRACT:deps.append({'id':fid+':accepted_registry','kind':'contract','owner':'L03','remediation':'Accept current chain125 deployment/ABI/codehash registry including pending beacon acceptance; no live action is performed by this gate'})
  for inherited in coverage['inherited_dependency_ids']:
   kind,owner,remediation=INHERITED[inherited];deps.append(dict(id=fid+':'+inherited,kind=kind,owner=owner,remediation=remediation))
  for name,kind,owner,remediation in SPECIAL.get(fid,[]):deps.append(dict(id=fid+':'+name,kind=kind,owner=owner,remediation=remediation))
  requirements=sorted(set(coverage['requirement_ids']+[rid]))
  requirement_owners=sorted({s['req'][r]['owner_lane'] for r in requirements})
  features.append(dict(inherited_dependency_ids=coverage['inherited_dependency_ids'], requirement_owner_lanes=requirement_owners,id=fid,title=coverage['title'],requirements=sorted(set(coverage['requirement_ids']+[rid])),owner_lanes=[f'L{i:02}' for i in OWNERS[fid]],prerequisites=deps,state='unqualified',scenarios={
   'success':req['ac_1'],
   'authorization_denial':f'{fid}: exercise an unauthorized actor against the real feature; assert denial and no unauthorized durable effect',
   'dependency_outage':f'{fid}: remove each mandatory dependency; assert explicit unavailable state, no fabricated data/settlement and bounded recovery',
   'reconnect_retry':f'{fid}: interrupt and retry the real user journey; reconcile persisted state and authority without duplicate effects or stale revision acceptance',
  }))
 return {'version':1,'features':features}
if __name__=='__main__':
 (pathlib.Path(__file__).parent/'inventory.json').write_text(json.dumps(build(),indent=2)+'\n')
