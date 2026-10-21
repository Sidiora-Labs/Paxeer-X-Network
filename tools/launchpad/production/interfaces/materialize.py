#!/usr/bin/env python3
"""Strict, deterministic catalog materializer. Proposed contracts, not handlers."""
import argparse, copy, hashlib, json, pathlib, re, sys, tomllib
DIALECT='https://json-schema.org/draft/2020-12/schema'
MAX256=str(2**256-1)
class ContractError(ValueError): pass

def bounded_uint(maximum, positive=False):
    n=len(maximum); alts=['[1-9][0-9]{0,%d}'%(n-2)]
    for i,d in enumerate(maximum):
        lo=1 if i==0 else 0; hi=int(d)-1
        if hi>=lo:
            digit=str(lo) if lo==hi else '[%d-%d]'%(lo,hi)
            alts.append(re.escape(maximum[:i])+digit+('[0-9]{%d}'%(n-i-1) if n-i-1 else ''))
    alts.append(maximum)
    if not positive: alts.insert(0,'0')
    return {'type':'string','pattern':'^(?:'+'|'.join(alts)+')$','maxLength':n}

SEMANTIC_DIGEST_VERSION = 'interface-catalog-v1'
SEMANTIC_SECTIONS = ('schema','endpoint','stream','error','interface_freeze','req')
LIFECYCLE_KEYS = {'status','lease','lease_id','leased_at','lease_expires_at','started_at','completed_at','updated_at'}
def semantic_input(spec):
    # Strip only top-level entry lifecycle metadata. Nested wire status remains normative.
    sections={}
    for section in SEMANTIC_SECTIONS:
        value=spec.get(section,{})
        if not isinstance(value,dict):raise ContractError('Normative section must be table: '+section)
        sections[section]={}
        for id,entry in value.items():
            sections[section][id]=({k:v for k,v in entry.items() if k not in LIFECYCLE_KEYS} if isinstance(entry,dict) else entry)
    return {'algorithm':SEMANTIC_DIGEST_VERSION,'sections':sections}
def semantic_digest(spec):
    return hashlib.sha256(json.dumps(semantic_input(spec),sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()

def ref(name): return {'$ref':'#/$defs/'+name}
def split_fields(text):
    result=[]; depth=0; start=0
    for i,c in enumerate(text):
        if c in '{[': depth+=1
        elif c in '}]': depth-=1
        if c==';' and depth==0: result.append(text[start:i].strip());start=i+1
    result.append(text[start:].strip())
    return [x for x in result if x and x!='none']

def wire_name(id,entry):
    if entry.get('wire_name'):return entry['wire_name']
    special={'IF_CONVENTIONS':'Conventions','IF_RESPONSE_ENVELOPE':'ResponseEnvelope','IF_CURSOR':'Cursor'}
    if id in special:return special[id]
    return ''.join(x.title() for x in id.removeprefix('IF_').split('_'))

def catalog_fields(v):
    if v.get('fields'):return v['fields']
    return ';'.join(v[k] for k in sorted(v,key=lambda x:int(x[6:]) if x.startswith('field_') else -1) if k.startswith('field_'))

class Compiler:
    def __init__(self,spec,supp,major=1,version=None,api_base=None):
        if type(major) is not int or major<1:raise ContractError("positive schema major required")
        self.major=major;self.version=version or str(major)+".0.0";self.api_base=api_base or "/v"+str(major)
        if self.version.split(".")[0]!=str(major) or self.api_base!="/v"+str(major):raise ContractError("version/API base mismatch")
        for endpoint in spec["endpoint"].values():
            if not endpoint["path"].startswith(self.api_base+"/"):raise ContractError("endpoint path outside configured API base: "+endpoint["path"])
        self.spec=spec;self.supp=supp;self.defs={};self.names={k:wire_name(k,v) for k,v in spec['schema'].items()};self.known=set(self.names.values())|set(supp['definitions'])
        self.domain_payloads=[]
        self.scalars()
    def scalars(self):
        self.defs.update({'UInt256':bounded_uint(MAX256),'PositiveUInt256':bounded_uint(MAX256,True),'AggregateUInt':{'type':'string','pattern':'^(0|[1-9][0-9]{0,155})$','maxLength':156},'SignedAggregateInteger':{'type':'string','pattern':'^(0|-?[1-9][0-9]{0,155})$','maxLength':157},'PriceComponent':{'type':'string','pattern':'^(0|[1-9][0-9]{0,511})$','maxLength':512},'PositivePriceComponent':{'type':'string','pattern':'^[1-9][0-9]{0,511}$','maxLength':512},'UInt63':bounded_uint(str(2**63-1))})
    def scalar_context(self,context,field):
        # Economic aggregate columns have wider bounds than individual chain values.
        aggregate={'volume24hRaw','volumeUsdlRaw','volumeTokenRaw','buyVolumeUsdlRaw','sellVolumeUsdlRaw','remainingCostUsdlRaw','knownRemainingCostUsdlRaw','pricedSubtotalUsdlRaw','completeValueUsdlRaw','knownRealizedSubtotalUsdlRaw','valuedBalanceUsdlRaw','indicativeTvlUsdlRaw','totalSupplyValuationUsdlRaw','circulatingMarketCapUsdlRaw','spentRaw','reservedRaw','metricRaw','earnedRaw','claimedRaw'}
        return 'AggregateUInt' if field in aggregate else 'UInt256'
    def parse_type(self,raw,context='',field=''):
        raw=raw.strip();raw=re.sub(r'\s+(path|header only|header)$','',raw)
        optional=raw.endswith('?');raw=raw[:-1] if optional else raw
        raw=raw.replace(' nullable','|null')
        default_match=re.search(r'\s+default(\d+)$',raw)
        if default_match:
            shape=self.parse_type(raw[:default_match.start()],context,field);shape['default']=int(default_match[1]);return shape
        if raw.endswith('|null'):
            return {'anyOf':[self.parse_type(raw[:-5],context,field),{'type':'null'}]}
        if raw=='null':return {'type':'null'}
        aliases={'uintString':self.scalar_context(context,field),'positiveUIntString':'PositiveUInt256','BigUInt512':'PriceComponent','positiveBigUInt512':'PositivePriceComponent','signedIntegerString':'SignedAggregateInteger','uint63String':'UInt63','opaqueResumeCursor':'OpaqueResumeCursor','queryAllowlisted':'TopicFilters'}
        if raw=='TopicSequenceVector<=20':return {'type':'array','items':ref('TopicSequence'),'minItems':1,'maxItems':20,'uniqueItems':True}
        if raw in aliases:return ref(aliases[raw])
        if raw in self.known or raw in self.defs:return ref(raw)
        if raw in ['declaredDomainPayload','domainPayload']:return {'oneOf':[ref(n) for n in self.domain_payloads]}
        if raw=='declaredTopicPayload':return {'anyOf':[ref(v['payload_schema']) for v in self.spec['stream'].values()]}
        if raw=='boolean':return {'type':'boolean'}
        m=re.fullmatch(r'boolean=(true|false)',raw)
        if m:return {'type':'boolean','const':m[1]=='true'}
        m=re.fullmatch(r'(?:string|integer|constant)=([A-Za-z0-9_./-]+)',raw)
        if m:
            v=m[1];return {'type':'integer','const':int(v)} if raw.startswith('integer=') else {'type':'string','const':v}
        if raw in ['string125']:return {'type':'string','const':'125'}
        if raw=='string>=0':return ref('UInt256')
        if raw=='string>=1':return ref('PositiveUInt256')
        if raw in ['UTC']:
            year=r'(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})'
            ordinary=r'(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))'
            leap=r'(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29'
            return {'type':'string','format':'date-time','pattern':'^(?:'+year+'-'+ordinary+'|'+leap+r')T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\.[0-9]{3}Z$'}
        if raw in ['UUID','UUIDv7']:return {'type':'string','format':'uuid',**({'pattern':r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$'} if raw=='UUIDv7' else {})}
        if raw in ['hex20','hex32','hex65']:return {'type':'string','pattern':'^0x[0-9a-fA-F]{%d}$'%({'hex20':40,'hex32':64,'hex65':130}[raw])}
        if raw=='lowercaseHex64charsWithoutPrefix':return {'type':'string','pattern':'^[0-9a-f]{64}$'}
        if raw=='hex64charsWithoutPrefix':return {'type':'string','pattern':'^[0-9a-fA-F]{64}$'}
        m=re.fullmatch(r'hexBytes<=(\d+)',raw)
        if m:return {'type':'string','pattern':'^0x(?:[0-9a-fA-F]{2})*$','maxLength':2+int(m[1])*2}
        if raw in ['finiteDecimalString','decimalString']:return {'type':'string','pattern':r'^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$','maxLength':512}
        m=re.fullmatch(r'(?:integer)?(\d+)\.\.(\d+)',raw)
        if m:return {'type':'integer','minimum':int(m[1]),'maximum':int(m[2])}
        m=re.fullmatch(r'integer>=(\d+)',raw)
        if m:return {'type':'integer','minimum':int(m[1]),'maximum':9007199254740991}
        if raw=='integer':return {'type':'integer','minimum':0,'maximum':9007199254740991}
        m=re.fullmatch(r'integer(\d+)',raw)
        if m:return {'type':'integer','const':int(m[1])}
        # Arrays use an explicit element contract, never unknown object items.
        m=re.fullmatch(r'(distinct)?(queryDistinctString|allowlistedString|[A-Za-z0-9]+)Array(?:(\d+)\.\.(\d+)|(?:<=|max)(\d+))?',raw)
        if m:
            elem=m[2];elem={'String':'string','string':'string','queryDistinctString':'string','allowlistedString':'allowlistedName','Hex20':'hex20'}.get(elem,elem)
            schema={'type':'array','items':self.parse_type(elem,context,field),'minItems':int(m[3] or 0),'maxItems':int(m[4] or m[5] or 100)}
            if m[1] or m[2]=='queryDistinctString':schema['uniqueItems']=True
            return schema
        if raw.endswith('[]'):
            return {'type':'array','items':self.parse_type(raw[:-2],context,field),'maxItems':100}
        if raw.startswith('{'):raise ContractError('Inline object must have supplementary named type: '+context+'.'+field)
        if raw in ['object','array','arraymax100','secretObject','mapBoolean','{start,end,timezone}','searchResult','feedEntry']:raise ContractError('Unresolved generic type '+raw+' at '+context+'.'+field)
        m=re.fullmatch(r'(?:UTF8|string)(?:length)?(\d+)\.\.(\d+)',raw)
        if m:return {'type':'string','minLength':int(m[1]),'maxLength':int(m[2])}
        m=re.fullmatch(r'(?:UTF8|string)(?:<=|max)(\d+)',raw)
        if m:return {'type':'string','maxLength':int(m[1])}
        if raw=='uppercaseSnake<=64':return {'type':'string','pattern':'^[A-Z][A-Z0-9_]*$','maxLength':64}
        if raw in ['httpsURL','configuredHttpsURI','allowedHttpsOrigin'] or raw.startswith('httpsURL<='):
            authority=r'[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?'
            suffix='' if raw=='allowedHttpsOrigin' else r'(?:[/?#][^\s]*)?'
            return {'type':'string','format':'uri','pattern':'^https://'+authority+suffix+'$','maxLength':2048}
        if raw in ['hostname','configuredHostname']:return {'type':'string','format':'hostname','maxLength':253}
        if raw=='BCP47':return {'type':'string','pattern':'^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$','maxLength':80}
        if raw=='IANA':return {'type':'string','pattern':'^(UTC|[A-Za-z_+-]+(?:/[A-Za-z0-9_+.-]+)+)$','maxLength':100}
        if raw=='relativeAllowlistedPath':return {'type':'string','pattern':'^'+re.escape(self.api_base)+'/[A-Za-z0-9_/:?&=.%,-]*$','maxLength':512}
        if raw in ['string','opaqueSecret','opaqueSigned','reauthProof','walletProof','deviceProof','allowlistedName','allowlistedMetric','allowlistedService']:
            return {'type':'string','minLength':1,'maxLength':4096 if raw in ['opaqueSecret','opaqueSigned','reauthProof','walletProof','deviceProof'] else 256}
        if '|' in raw or ',' in raw:
            values=re.split(r'[|,]',raw)
            if all(re.fullmatch(r'[A-Za-z0-9_./-]+',x) for x in values):return {'type':'string','enum':values}
        literals={'json','live','active','disclosed_assistant','subscribe','text/event-stream'}
        if raw in literals:return {'type':'string','const':raw}
        raise ContractError('Unknown field type '+repr(raw)+' at '+context+'.'+field)
    def object_fields(self,text,context,endpoint=False):
        props={};required=[]
        for entry in split_fields(text):
            if ':' not in entry:raise ContractError('Untyped field '+entry+' at '+context)
            name,typ=entry.split(':',1);name=name.strip();typ=typ.strip()
            if name in props:raise ContractError('Duplicate field '+context+'.'+name)
            override=self.supp['endpointOverrides' if endpoint else 'fieldOverrides'].get(context+'.'+name)
            if override:typ=override
            props[name]=self.parse_type(typ,context,name)
            if not typ.endswith('?'):required.append(name)
        return {'type':'object','properties':props,'required':required,'additionalProperties':False}
    def build(self):
        self.defs['OpaqueResumeCursor']={'type':'string','minLength':1,'maxLength':2048,'pattern':'^[A-Za-z0-9_.-]+$'}
        for name,text in self.supp['definitions'].items():
            self.defs[name]=self.parse_type(text,name) if ':' not in text else self.object_fields(text,name)
        self.defs['Conventions']=self.object_fields('chainId:string=125; nativeSymbol:constant=PAX; nativeDecimals:integer=18; quoteSymbol:constant=USDL; quoteDecimals:integer=6; creationFeeRaw:constant=1000000; finality:constant=committed; naming:constant=camelCase','Conventions')
        for id,v in self.spec['schema'].items():
            name=self.names[id]
            if name in ['Conventions','ResponseEnvelope']:continue
            self.defs[name]=self.object_fields(catalog_fields(v),name)
            self.defs[name]['additionalProperties']=name not in ['Subscribe','Cursor','ResumeCursor']
            self.defs[name]['description']=v.get('semantics',v.get('scope','Proposed '+name))
        self.defs['EoaWalletProof']=self.object_fields('kind:constant=eoa_eip191; wallet:hex20; signature:hex65','EoaWalletProof')
        self.defs['Erc1271WalletProof']=self.object_fields('kind:constant=erc1271; wallet:hex20; signature:hexBytes<=4096','Erc1271WalletProof')
        self.defs['WalletProof']={'oneOf':[ref('EoaWalletProof'),ref('Erc1271WalletProof')]}
        stream_event_names={}
        for sid,entry in self.spec['stream'].items():
            event_name=''.join(x.title() for x in sid.removeprefix('IF_').split('_'))+'Event'
            event=copy.deepcopy(self.defs['StreamEvent'])
            event['properties']['payload']=ref(entry['payload_schema'])
            pattern=re.escape(entry['topic'])
            for parameter in re.findall(r'\{(\w+)\}',entry['topic']):
                if parameter in ['poolAddress','wallet']:replacement='0x[0-9a-fA-F]{40}'
                elif parameter=='interval':replacement='(?:1m|5m|15m|1h|4h|1d|1w)'
                elif parameter=='category':replacement='(?:trending|breakout|volume|gainers|losers|movers|unusual|new)'
                else:replacement='[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'
                pattern=pattern.replace(re.escape('{'+parameter+'}'),replacement)
            event['properties']['topic']={'type':'string','pattern':'^'+pattern+'$','maxLength':200}
            self.defs[event_name]=event;stream_event_names[sid]=event_name
        self.defs['StreamEvent']={'oneOf':[ref(name) for name in stream_event_names.values()]}
        mappings={};openpaths={}
        for id,v in self.spec['endpoint'].items():
            base=''.join(x.title() for x in id.removeprefix('IF_').split('_'));req=base+'Request';data=base+'Data';response=base+'Response'
            text=v['request_fields'];pathfields=re.findall(r'\{(\w+)\}',v['path'])
            special={'IF_REFRESH':[], 'IF_EVENTS_SSE':[], 'IF_ACCOUNT_DELETE_STATUS':['jobId:UUID'],'IF_STREAM_TICKET_ISSUE':['topics:distinctStringArray1..20','schemaMajor:integer='+str(self.major)]}
            entries=(split_fields(text) if self.major!=1 and id=='IF_STREAM_TICKET_ISSUE' else special.get(id,split_fields(text)));request=self.object_fields(';'.join(entries),id,True)
            # Path fields not repeated in request text are still required and explicitly typed.
            for n in pathfields:
                if n not in request['properties']:
                    if n.endswith('Address') or n=='wallet':typ='hex20'
                    elif n=='transactionHash':typ='hex32'
                    elif n in ['category']:raise ContractError('Category path requires explicit enum: '+id)
                    else:typ='UUID'
                    request['properties'][n]=self.parse_type(typ,id,n);request['required'].append(n)
            if v['method']=='GET':
                for n,shape in request['properties'].items():
                    nullable=any(x.get('type')=='null' for x in shape.get('anyOf',[]))
                    if (nullable or n=='limit' or 'default' in shape) and n not in pathfields:
                        if n in request['required']:request['required'].remove(n)
                    if n=='limit':shape.setdefault('default',min(50,shape.get('maximum',50)))
            self.defs[req]=request
            empty=v['response_fields'] in ['empty','empty204'];sse=id=='IF_EVENTS_SSE'
            if not empty and not sse:
                self.defs[data]=self.object_fields(v['response_fields'],id,True)
                self.defs[data]['additionalProperties']=True
                self.domain_payloads.append(data)
                env=self.envelope(ref(data));self.defs[response]=env
            parameters=[];bodyprops={};bodyrequired=[]
            for n,shape in request['properties'].items():
                location='path' if n in pathfields else ('query' if v['method']=='GET' else 'body')
                if location=='body':bodyprops[n]=shape;bodyrequired+=([n] if n in request['required'] else [])
                else:parameters.append({'name':n,'in':location,'required':n in request['required'],'schema':shape,**({'style':'form','explode':False} if shape.get('type')=='array' else {})})
            if id=='IF_REFRESH':parameters += [{'name':'sidiora_refresh','in':'cookie','required':True,'schema':{'type':'string','maxLength':4096}}, {'name':'X-CSRF-Token','in':'header','required':True,'schema':{'type':'string','maxLength':256}}]
            if id=='IF_ACCOUNT_DELETE_STATUS':parameters.append({'name':'X-Recovery-Receipt','in':'header','required':False,'schema':{'type':'string','maxLength':4096}})
            if id in ['IF_REFRESH','IF_STREAM_TICKET_ISSUE']:parameters.append({'name':'Origin','in':'header','required':True,'schema':{'type':'string','format':'uri','pattern':'^https://','maxLength':2048}})
            if id=='IF_STREAM_TICKET_ISSUE':parameters.append({'name':'X-CSRF-Token','in':'header','required':False,'schema':{'type':'string','maxLength':256},'description':'Required when cookie-authenticated; validated against session.'})
            if id=='IF_EVENTS_SSE':parameters += [{'name':'topics','in':'query','required':True,'schema':self.parse_type('distinctStringArray1..20'),'style':'form','explode':False},{'name':'Last-Event-ID','in':'header','required':False,'schema':ref('OpaqueResumeCursor')},{'name':'Accept','in':'header','required':True,'schema':{'type':'string','const':'text/event-stream'}}]
            code=str(v['success_status']);responses={code:{'description':'Proposed success; '+v['response_fields']}}
            if sse:responses[code]['content']={'text/event-stream':{'schema':{'type':'string','description':'UTF8 SSE frames; data validates StreamEvent or StreamControl, id is signed cursor checkpoint.'}}}
            elif not empty:responses[code]['content']={'application/json':{'schema':ref(response)}}
            for err in self.spec['error'].values():responses.setdefault(str(err['http_status']),{'description':'Error codes: '+', '.join(e['code'] for e in self.spec['error'].values() if e['http_status']==err['http_status']),'content':{'application/json':{'schema':ref('Error')}}})
            operation={'operationId':id,'summary':v.get('authorization',id),'parameters':parameters,'responses':responses,'security':self.security(id,v['auth']),'x-handler-status':'proposed_not_implemented','x-catalog-id':id,'x-request-body-limit-bytes':v.get('request_body_limit_bytes',v.get('body_limit_bytes',16384)),'x-timeout-ms':v.get('timeout_ms',1000),'x-authorization':v.get('authorization',''),'x-idempotency':v.get('idempotency',''),'x-cache-policy':v.get('cache','')}
            if bodyprops:
                bodyname=base+'Body';self.defs[bodyname]={'type':'object','properties':bodyprops,'required':bodyrequired,'additionalProperties':False};operation['requestBody']={'required':bool(bodyrequired),'content':{'application/json':{'schema':ref(bodyname)}}}
            openpaths.setdefault(v['path'],{})[v['method'].lower()]=operation
            if empty:
                self.defs[data]={'type':'null','description':'No HTTP response body for empty-status endpoint'}
                self.defs[response]={'type':'null','description':'No HTTP response body for empty-status endpoint'}
            elif sse:
                self.defs[data]={'anyOf':[ref('StreamEvent'),ref('StreamControl')]}
                self.defs[response]={'anyOf':[ref('StreamEvent'),ref('StreamControl')],'description':'Per-event JSON payload inside SSE frame; not ResponseEnvelope'}
            mappings[id]={'request':req,'data':data,'response':response,'method':v['method'],'path':v['path'],'wireMode':'sse' if sse else ('empty' if empty else 'envelope')}
        self.defs['ResponseEnvelope']=self.envelope({'anyOf':[ref(n) for n in self.domain_payloads]})
        # Resolve deferred domain payload unions; no unconstrained schema may survive.
        for name,v in list(self.defs.items()):
            if name=='ResponseEnvelope':continue
            self.defs[name]=self.replace_empty_union(v)
        self.strictify_requests(mappings)
        for id,mapping in mappings.items():
            operation=openpaths[mapping['path']][mapping['method'].lower()]
            request=self.defs[mapping['request']]
            for parameter in operation['parameters']:
                if parameter['name'] in request.get('properties',{}):parameter['schema']=copy.deepcopy(request['properties'][parameter['name']])
        self.bound_safe_integers()
        root={'$schema':DIALECT,'$defs':dict(sorted(self.defs.items()))}
        api={'openapi':'3.1.0','jsonSchemaDialect':DIALECT,'info':{'title':'KindleLaunch proposed production interfaces','version':self.version,'description':'Materialized design contract; handler, provider, deployment and load qualification not implied.'},'servers':[{'url':'https://api.example.invalid'}],'paths':openpaths,'components':{'schemas':copy.deepcopy(root['$defs']),'securitySchemes':{'bearerAuth':{'type':'http','scheme':'bearer'},'sessionCookie':{'type':'apiKey','in':'cookie','name':'sidiora_session'},'refreshCookie':{'type':'apiKey','in':'cookie','name':'sidiora_refresh'},'providerSignature':{'type':'apiKey','in':'header','name':'X-Provider-Signature'},'recoveryReceipt':{'type':'apiKey','in':'header','name':'X-Recovery-Receipt'},'viewerToken':{'type':'apiKey','in':'header','name':'X-Viewer-Token'}}}}
        api=replace_refs(api,'#/components/schemas/')
        channels={};messages={};operations={}
        for id,v in self.spec['stream'].items():
            messages[id]={'name':id,'title':v['topic'],'payload':ref(stream_event_names[id]),'x-payload-schema':v['payload_schema']}
            channels[id]={'address':v['topic'],'messages':{id:{'$ref':'#/components/messages/'+id}},'parameters':{n:{'description':'Validated '+n+' subscription parameter'} for n in re.findall(r'\{(\w+)\}',v['topic'])}}
            operations[id]={'action':'send','channel':{'$ref':'#/channels/'+id},'messages':[{'$ref':'#/channels/'+id+'/messages/'+id}],'summary':v['semantics']}
        channels['StreamControl']={'address':self.api_base+'/stream','messages':{'Subscribe':{'$ref':'#/components/messages/Subscribe'},'Control':{'$ref':'#/components/messages/Control'}}}
        messages['Subscribe']={'payload':ref('Subscribe')};messages['Control']={'payload':ref('StreamControl')}
        operations['receiveSubscribe']={'action':'receive','channel':{'$ref':'#/channels/StreamControl'},'messages':[{'$ref':'#/channels/StreamControl/messages/Subscribe'}]}
        operations['sendControl']={'action':'send','channel':{'$ref':'#/channels/StreamControl'},'messages':[{'$ref':'#/channels/StreamControl/messages/Control'}]}
        asyncapi={'asyncapi':'3.0.0','info':{'title':'KindleLaunch proposed streams','version':self.version},'defaultContentType':'application/json','servers':{'browser':{'host':'api.example.invalid','pathname':self.api_base+'/stream','protocol':'wss'}},'channels':channels,'operations':operations,'components':{'schemas':copy.deepcopy(root['$defs']),'messages':messages}}
        asyncapi=replace_refs(asyncapi,'#/components/schemas/')
        mapping={'schemaVersion':1,'status':'materialized_design_not_runtime_qualified','schemas':{k:{'wireName':self.names[k],'ref':'schema-definitions.json#/$defs/'+self.names[k]} for k in self.spec['schema']},'endpoints':mappings,'streams':{k:{'topic':v['topic'],'payload':v['payload_schema'],'eventEnvelope':stream_event_names[k]} for k,v in self.spec['stream'].items()},'errors':{k:{'code':v['code'],'httpStatus':v['http_status'],'schema':'Error'} for k,v in self.spec['error'].items()},'inputs':{'semanticDigestAlgorithm':SEMANTIC_DIGEST_VERSION,'catalogSha256':semantic_digest(self.spec),'supplementarySha256':hashlib.sha256(json.dumps(self.supp,sort_keys=True).encode()).hexdigest()}}
        return root,api,asyncapi,mapping
    def bound_safe_integers(self):
        def walk(value):
            if isinstance(value,dict):
                if value.get('type')=='integer':
                    value.setdefault('minimum',-9007199254740991)
                    value.setdefault('maximum',9007199254740991)
                    if value['minimum'] < -9007199254740991 or value['maximum'] > 9007199254740991:raise ContractError('Unsafe JSON integer bounds')
                for child in value.values():walk(child)
            elif isinstance(value,list):
                for child in value:walk(child)
        walk(self.defs)
    def security(self,id,auth):
        if id in ['IF_CHALLENGE','IF_LOGIN']:return []
        if id=='IF_REFRESH':return [{'refreshCookie':[]}]
        if id=='IF_LIVE_WEBHOOK':return [{'providerSignature':[]}]
        if id=='IF_ACCOUNT_DELETE_STATUS':return [{'sessionCookie':[]},{'bearerAuth':[]},{'recoveryReceipt':[]}]
        if id=='IF_STREAM_HEARTBEAT':return [] # signed viewerToken is explicitly validated request-body proof
        if auth=='public' or auth.startswith('public_visibility'):return []
        if id in ['IF_STREAM_TICKET_ISSUE','IF_TOPIC_SNAPSHOT_READ','IF_EVENTS_SSE']:return [{},{'sessionCookie':[]},{'bearerAuth':[]}]
        return [{'sessionCookie':[]},{'bearerAuth':[]}]
    def strictify_requests(self,mappings):
        clones={}
        def strict(shape):
            if isinstance(shape,list):return [strict(x) for x in shape]
            if not isinstance(shape,dict):return shape
            if '$ref' in shape:
                name=shape['$ref'].removeprefix('#/$defs/');target=self.defs[name]
                if target.get('type')=='object' or 'anyOf' in target or 'oneOf' in target:
                    clone='Input'+name
                    if clone not in clones:
                        clones[clone]=None
                        clones[clone]=strict(target)
                    return ref(clone)
                return copy.deepcopy(shape)
            result={k:strict(v) for k,v in shape.items()}
            if result.get('type')=='object':result['additionalProperties']=False
            return result
        for m in mappings.values():
            self.defs[m['request']]=strict(self.defs[m['request']])
            body=m['request'].removesuffix('Request')+'Body'
            if body in self.defs:self.defs[body]=strict(self.defs[body])
        self.defs.update(clones)
    def replace_empty_union(self,v):
        if isinstance(v,dict):
            if v.get('oneOf')==[]:return {'anyOf':[ref(n) for n in self.domain_payloads]}
            return {k:self.replace_empty_union(x) for k,x in v.items()}
        if isinstance(v,list):return [self.replace_empty_union(x) for x in v]
        return v
    def envelope(self,data):
        return {'type':'object','properties':{'requestId':{'type':'string','format':'uuid'},'data':data,'watermark':{'anyOf':[ref('Watermark'),{'type':'null'}]},'revision':{'anyOf':[ref('PositiveUInt256'),{'type':'null'}]},'quality':{'enum':['complete','partial','stale','quarantined'],'type':'string'},'nextCursor':{'type':['string','null'],'maxLength':2048},'generatedAt':self.parse_type('UTC')},'required':['requestId','data','watermark','revision','quality','nextCursor','generatedAt'],'additionalProperties':True}

def replace_refs(value,prefix):
    if isinstance(value,dict):return {k:(prefix+v.removeprefix('#/$defs/') if k=='$ref' and isinstance(v,str) and v.startswith('#/$defs/') else replace_refs(v,prefix)) for k,v in value.items()}
    if isinstance(value,list):return [replace_refs(v,prefix) for v in value]
    return value

def validate_refs(doc):
    def walk(v):
        if isinstance(v,dict):
            if '$ref' in v:
                pointer=v['$ref']
                if not pointer.startswith('#/'):raise ContractError('Nonlocal ref '+pointer)
                target=doc
                for part in pointer[2:].split('/'):
                    try:target=target[part.replace('~1','/').replace('~0','~')]
                    except (KeyError,TypeError):raise ContractError('Unresolved ref '+pointer)
            for x in v.values():walk(x)
        elif isinstance(v,list):
            for x in v:walk(x)
    walk(doc)

def generate(spec,supp,major=1,version=None,api_base=None):
    root,api,asyncapi,mapping=Compiler(spec,supp,major,version,api_base).build()
    for doc in (root,api,asyncapi):validate_refs(doc)
    from jsonschema import Draft202012Validator
    Draft202012Validator.check_schema(root)
    files={'schema-definitions.json':root,'openapi.json':api,'asyncapi.json':asyncapi,'catalog-map.json':mapping}
    for id,v in mapping['schemas'].items():
        needed={}
        def collect(name):
            if name in needed:return
            needed[name]=root['$defs'][name]
            def visit(value):
                if isinstance(value,dict):
                    if '$ref' in value:collect(value['$ref'].removeprefix('#/$defs/'))
                    for child in value.values():visit(child)
                elif isinstance(value,list):
                    for child in value:visit(child)
            visit(needed[name])
        collect(v['wireName'])
        files['schemas/'+id+'.json']={'$schema':DIALECT,'$ref':'#/$defs/'+v['wireName'],'$defs':dict(sorted(needed.items()))}
    files['inputs/catalog.json' if major==1 else 'catalog.json']=semantic_input(spec)
    for name,document in files.items():
        if name.startswith('schemas/'):
            validate_refs(document);Draft202012Validator.check_schema(document)
    return {name:(json.dumps(v,indent=2,sort_keys=True,ensure_ascii=False)+'\n').encode() for name,v in files.items()}

def validate_documents(files,inputs):
    from jsonschema import Draft202012Validator, Draft7Validator, RefResolver, FormatChecker
    documents={name:json.loads(data) for name,data in files.items() if name in ['schema-definitions.json','openapi.json','asyncapi.json']}
    provenance=json.load(open(inputs/'standards-provenance.json'))
    for source in provenance['sources']:
        if hashlib.sha256((inputs/source['file']).read_bytes()).hexdigest()!=source['sha256']:raise ContractError('Standards schema provenance hash mismatch: '+source['file'])
    openapi=json.load(open(inputs/'openapi-3.1-document.schema.json'))
    Draft202012Validator(openapi,format_checker=FormatChecker()).validate(documents['openapi.json'])
    asyncschema=json.load(open(inputs/'asyncapi-3.0.0-document.schema.json'));store={}
    def ids(value):
        if isinstance(value,dict):
            if isinstance(value.get('$id'),str):store[value['$id']]=value
            for child in value.values():ids(child)
        elif isinstance(value,list):
            for child in value:ids(child)
    ids(asyncschema)
    def offline_only(uri):raise ContractError('Standards validation attempted network resolution: '+uri)
    resolver=RefResolver.from_schema(asyncschema,store=store,handlers={'http':offline_only,'https':offline_only})
    Draft7Validator(asyncschema,resolver=resolver,format_checker=FormatChecker()).validate(documents['asyncapi.json'])
    Draft202012Validator.check_schema(documents['schema-definitions.json'])
    return 'official bundled OpenAPI3.1/AsyncAPI3.0 document schemas + JSONSchema2020-12 meta-schema'

def main():
    p=argparse.ArgumentParser();p.add_argument('--spec',default='launchpad/spec/production-system/spec.kvx');p.add_argument('--out',default='launchpad/spec/production-system/integration/contracts/v1');p.add_argument('--check',action='store_true');p.add_argument('--validate',action='store_true');p.add_argument('--major',type=int,default=1);p.add_argument('--version');p.add_argument('--api-base');p.add_argument('--catalog');p.add_argument('--supplementary');a=p.parse_args();out=pathlib.Path(a.out)
    if a.major!=1 and out.resolve()==pathlib.Path('launchpad/spec/production-system/integration/contracts/v1').resolve():raise ContractError('version2 generation cannot overwrite v1')
    spec=json.load(open(a.catalog)) if a.catalog else tomllib.load(open(a.spec,'rb'))
    if 'sections' in spec:spec=spec['sections']
    supp=json.load(open(a.supplementary or (out/'inputs/supplementary.json' if a.major==1 else out/'supplementary.json')));files=generate(spec,supp,a.major,a.version,a.api_base)
    if a.validate:print('PASS '+validate_documents(files,out/'inputs'))
    if a.check or a.validate:
        for name,data in files.items():
            path=out/name
            if not path.exists() or path.read_bytes()!=data:raise ContractError('Generated artifact missing or stale: '+str(path))
        print('PASS deterministic regeneration/reference/schema validation: '+str(len(files))+' artifacts; '+str(len(spec['schema']))+' schemas/'+str(len(spec['endpoint']))+' endpoints/'+str(len(spec['stream']))+' streams/'+str(len(spec['error']))+' errors')
    else:
        for name,data in files.items():path=out/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(data)
        print('Materialized '+str(len(files))+' design artifacts; handlers not implemented')
if __name__=='__main__':
    try:main()
    except (ContractError,ValueError,KeyError) as e:print('FAIL: '+str(e),file=sys.stderr);sys.exit(1)
