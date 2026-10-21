#!/usr/bin/env python3
"""Deterministic closed JSON Schema -> Go/TypeScript contracts. --check never writes."""
import argparse, hashlib, json, pathlib, re, subprocess, sys

class GenerationError(ValueError): pass

FROZEN_V1_GENERATOR_SOURCE = b'#!/usr/bin/env python3\n"""Deterministic closed JSON Schema -> Go/TypeScript contracts. --check never writes."""\nimport argparse, hashlib, json, pathlib, re, subprocess, sys\n\nclass GenerationError(ValueError): pass\nKEYWORDS={\'$schema\',\'$id\',\'$ref\',\'$defs\',\'type\',\'title\',\'description\',\'default\',\'examples\',\'example\',\'deprecated\',\'readOnly\',\'writeOnly\',\'const\',\'enum\',\'oneOf\',\'anyOf\',\'allOf\',\'not\',\'properties\',\'required\',\'additionalProperties\',\'items\',\'minItems\',\'maxItems\',\'uniqueItems\',\'minLength\',\'maxLength\',\'pattern\',\'format\',\'minimum\',\'maximum\',\'exclusiveMinimum\',\'exclusiveMaximum\',\'multipleOf\',\'minProperties\',\'maxProperties\'}\nFORMATS={\'date-time\',\'uuid\',\'uri\',\'email\',\'hostname\',\'ipv4\',\'ipv6\'}\ndef ident(s):\n value=\'\'.join(p[:1].upper()+p[1:] for p in re.split(\'[^a-zA-Z0-9]\',s) if p)\n if not value or value[0].isdigit():value=\'N\'+value\n return value\n\ndef validate_schema(node,defs,path):\n if not isinstance(node,dict):raise GenerationError(f\'{path}: boolean/nonobject schema unsupported; define a closed concrete schema\')\n for k in node:\n  if k not in KEYWORDS and not k.startswith(\'x-\'):raise GenerationError(f\'{path}: unsupported schema keyword {k}\')\n if \'$ref\' in node:\n  ref=node[\'$ref\']\n  if not ref.startswith(\'#/$defs/\') or ref[8:] not in defs:raise GenerationError(f\'{path}: unresolved/nonlocal ref {ref}\')\n if node.get(\'format\') and node[\'format\'] not in FORMATS:raise GenerationError(f\'{path}: unsupported format\')\n if node.get(\'type\')==\'object\' and node.get(\'additionalProperties\') is not False and not node.get(\'properties\'):\n  raise GenerationError(f\'{path}: open generic object requires explicit typed properties or bounded dictionary schema\')\n if node.get(\'type\')==\'array\' and not isinstance(node.get(\'items\'),dict):raise GenerationError(f\'{path}: typed array items required\')\n if \'pattern\' in node:\n  # Generated Go uses RE2; incompatible regex must not silently change semantics.\n  if re.search(r\'\\(\\?[=!<]|\\\\[1-9]\',node[\'pattern\']):raise GenerationError(f\'{path}: pattern not portable to RE2\')\n for k in (\'oneOf\',\'anyOf\',\'allOf\'):\n  for i,sub in enumerate(node.get(k,[])):validate_schema(sub,defs,f\'{path}/{k}/{i}\')\n if \'not\' in node:validate_schema(node[\'not\'],defs,path+\'/not\')\n if len({ident(k) for k in node.get(\'properties\',{})})!=len(node.get(\'properties\',{})):raise GenerationError(f\'{path}: Go property identifier collision\')\n for k,sub in node.get(\'properties\',{}).items():validate_schema(sub,defs,path+\'/\'+k)\n if \'items\' in node:validate_schema(node[\'items\'],defs,path+\'/items\')\n\ndef nullable(node):\n typ=node.get(\'type\')\n if isinstance(typ,list) and \'null\' in typ:\n  rest=[t for t in typ if t!=\'null\'];return dict(node,type=rest[0] if len(rest)==1 else rest)\n for key in (\'anyOf\',\'oneOf\'):\n  if key in node:\n   arr=node[key];remaining=[n for n in arr if n.get(\'type\')!=\'null\']\n   if len(remaining)!=len(arr):return remaining[0] if len(remaining)==1 else {key:remaining}\n return None\n\nclass Generator:\n def __init__(self,document):self.document=document;self.nodes=dict(document[\'$defs\']);self.output={};self.unions={};self.used=set(self.nodes)\n def lift(self,node,hint):\n  name=hint;counter=2\n  while name in self.used:\n   if self.nodes.get(name)==node:return name\n   name=hint+str(counter);counter+=1\n  self.used.add(name);self.nodes[name]=node;return name\n def type(self,node,hint,lang):\n  n=nullable(node)\n  if n is not None:\n   inner=self.type(n,hint+\'NonNull\',lang);return \'*\'+inner if lang==\'go\' else inner+\' | null\'\n  if \'$ref\' in node:return node[\'$ref\'][8:]\n  if any(k in node for k in (\'oneOf\',\'anyOf\')):\n   name=self.lift(node,hint);return name\n  if \'allOf\' in node:raise GenerationError(\'allOf types require materialized flattening before generation\')\n  typ=node.get(\'type\')\n  if not typ:\n   if \'const\' in node:typ={str:\'string\',bool:\'boolean\',int:\'integer\',type(None):\'null\'}.get(type(node[\'const\']))\n   elif node.get(\'enum\'):typ=type(node[\'enum\'][0]);typ={str:\'string\',int:\'integer\',bool:\'boolean\'}.get(typ)\n  if isinstance(typ,list):raise GenerationError(f\'{hint}: nonnullable multi-type must be explicit variants\')\n  if typ==\'object\':\n   if lang==\'ts\':return \'{ \'+ \'; \'.join(json.dumps(k)+(\'\' if k in node.get(\'required\',[]) else \'?\')+\': \'+self.type(v,hint+ident(k),\'ts\') for k,v in node.get(\'properties\',{}).items())+\' }\'\n   return \'struct {\\n\'+\'\'.join(\'\\t\'+ident(k)+\' \'+(self.type(v,hint+ident(k),\'go\') if k in node.get(\'required\',[]) else \'Optional[\'+self.type(v,hint+ident(k),\'go\')+\']\')+\' `json:\'+json.dumps(k+(\'\' if k in node.get(\'required\',[]) else \',omitzero\'))+\'`\\n\' for k,v in node.get(\'properties\',{}).items())+\'}\'\n  if typ==\'array\':return (\'[]\'+self.type(node[\'items\'],hint+\'Item\',\'go\')) if lang==\'go\' else \'Array<\'+self.type(node[\'items\'],hint+\'Item\',\'ts\')+\'>\'\n  if lang==\'ts\' and (\'enum\' in node or \'const\' in node):return \' | \'.join(json.dumps(v) for v in node.get(\'enum\',[node.get(\'const\')]))\n  values={\'string\':(\'string\',\'string\'),\'integer\':(\'SafeInteger\',\'number\'),\'number\':(\'float64\',\'number\'),\'boolean\':(\'bool\',\'boolean\'),\'null\':(\'NullValue\',\'null\')}\n  if typ not in values:raise GenerationError(f\'{hint}: no concrete type for {node}\')\n  return values[typ][lang==\'ts\']\n def emit(self):\n  # Iteration admits lifted nested unions while preserving deterministic insertion order.\n  go=[];ts=[];done=set()\n  while len(done)<len(self.nodes):\n   name=next(n for n in self.nodes if n not in done);done.add(name);node=self.nodes[name]\n   if not re.fullmatch(\'[A-Z][A-Za-z0-9]*\',name):raise GenerationError(f\'invalid definition identifier {name}\')\n   branch_key=next((k for k in (\'oneOf\',\'anyOf\') if k in node),None)\n   nonnull=nullable(node)\n   if branch_key and nonnull is None:\n    branches=node[branch_key];types=[self.type(v,name+\'Variant\'+str(i),\'go\') for i,v in enumerate(branches,1)]\n    go.append(\'type \'+name+\' struct {\\n\'+\'\'.join(f\'\\tVariant{i} *{t} `json:"-"`\\n\' for i,t in enumerate(types,1))+\'}\\n\')\n    rawschema=json.dumps(json.dumps(node,separators=(\',\',\':\')))\n    go.append(f\'func (v *{name}) UnmarshalJSON(raw []byte) error {{\\n if err:=validateNode(json.RawMessage({rawschema}),raw,"$");err!=nil {{return err}}\\n *v={name}{{}}\\n\')\n    for i,t in enumerate(types,1):\n     branch=json.dumps(json.dumps(branches[i-1],separators=(\',\',\':\')))\n     go.append(f\' if validateNode(json.RawMessage({branch}),raw,"$")==nil {{var value {t};if err:=json.Unmarshal(raw,&value);err!=nil{{return err}};v.Variant{i}=&value;return nil}}\\n\')\n    go.append(\'return fmt.Errorf("no union variant")\\n}\\n\')\n    go.append(f\'func (v {name}) MarshalJSON() ([]byte,error) {{\\n count:=0;var raw []byte;var err error\\n\')\n    for i in range(1,len(types)+1):go.append(f\' if v.Variant{i}!=nil{{count++;raw,err=json.Marshal(v.Variant{i});if err!=nil{{return nil,err}}}}\\n\')\n    go.append(\'if count!=1{return nil,fmt.Errorf("union requires exactly one selected variant")};\'+f\'if err:=validateNode(json.RawMessage({rawschema}),raw,"$");err!=nil{{return nil,err}};return raw,nil\\n}}\\n\')\n    ts.append(\'export type \'+name+\' = \'+\' | \'.join(self.type(v,name+\'Variant\'+str(i),\'ts\') for i,v in enumerate(branches,1))+\';\\n\')\n   else:\n    go.append(\'type \'+name+(\' = \' if \'$ref\' in node or node.get(\'type\') in (\'null\',\'integer\') else \' \')+self.type(node,name,\'go\')+\'\\n\');ts.append(\'export type \'+name+\' = \'+self.type(node,name,\'ts\')+\';\\n\')\n  # The runtime schema bundle also includes lifted named variants.\n  doc=dict(self.document);doc[\'$defs\']=self.nodes\n  schema=json.dumps(doc,sort_keys=True,indent=2)+\'\\n\';digest=hashlib.sha256(schema.encode()).hexdigest()\n  go_header=\'// Code generated by tools/production/interfaces/codegen.py; DO NOT EDIT.\\n// Schema SHA256: \'+digest+\'\\npackage v1\\n\\nimport ("bytes";"encoding/json";"fmt")\\n\\ntype NullValue struct{}\\nfunc (NullValue) MarshalJSON()([]byte,error){return []byte("null"),nil}\\nfunc (*NullValue) UnmarshalJSON(raw []byte)error{if string(bytes.TrimSpace(raw))!="null"{return fmt.Errorf("expected null")};return nil}\\n\'\n  # Keep imports used even if a tiny synthetic schema has no union.\n  go_header+=\'var _ = json.Valid\\nvar _ = fmt.Errorf\\n\'\n  go_header+=\'type Optional[T any] struct { Present bool; Value T }\\nfunc (v Optional[T]) IsZero()bool{return !v.Present}\\nfunc (v Optional[T]) MarshalJSON()([]byte,error){if !v.Present{return nil,fmt.Errorf("absent optional value cannot be serialized directly")};return json.Marshal(v.Value)}\\nfunc (v *Optional[T]) UnmarshalJSON(raw []byte)error{var value T;if err:=json.Unmarshal(raw,&value);err!=nil{return err};v.Value=value;v.Present=true;return nil}\\n\'\n  go_header+=\'type SafeInteger int64\\nfunc (v SafeInteger) MarshalJSON()([]byte,error){if v < -9007199254740991 || v > 9007199254740991{return nil,fmt.Errorf("integer outside JS safe range")};return json.Marshal(int64(v))}\\nfunc (v *SafeInteger) UnmarshalJSON(raw []byte)error{r,err:=rational(raw);if err!=nil{return err};if !r.IsInt()||!r.Num().IsInt64(){return fmt.Errorf("integer out of range")};n:=r.Num().Int64();if n < -9007199254740991 || n > 9007199254740991{return fmt.Errorf("integer outside JS safe range")};*v=SafeInteger(n);return nil}\\n\'\n  decoders=[]\n  for name in sorted(self.document[\'$defs\']):decoders.append(f\'func Decode{name}(raw []byte) ({name},error) {{var value {name};if err:=ValidateJSON("{name}",raw);err!=nil{{return value,err}};err:=json.Unmarshal(raw,&value);return value,err}}\\n\')\n  gocode=go_header+\'\'.join(go)+\'\'.join(decoders)\n  fmt=subprocess.run([\'gofmt\'],input=gocode,text=True,capture_output=True)\n  if fmt.returncode:raise GenerationError(\'gofmt: \'+fmt.stderr)\n  validators=(pathlib.Path(__file__).parent/\'validator.go.txt\').read_text().replace(\'__SCHEMA_JSON__\',json.dumps(schema))\n  vf=subprocess.run([\'gofmt\'],input=validators,text=True,capture_output=True)\n  if vf.returncode:raise GenerationError(\'validator gofmt: \'+vf.stderr)\n  ts_header=\'// Code generated; DO NOT EDIT. Schema SHA256: \'+digest+\'\\n\'\n  ts.append(\'export interface ContractTypes {\\n\'+\'\'.join(json.dumps(n)+\': \'+n+\';\\n\' for n in sorted(self.document[\'$defs\']))+\'}\\n\')\n  return {\'shared/interfaces/v1/types.gen.go\':fmt.stdout,\'shared/interfaces/v1/validators.gen.go\':vf.stdout,\'client/packages/production-contracts/src/types.gen.ts\':ts_header+\'\'.join(ts),\'client/packages/production-contracts/src/schemas.gen.json\':schema}\n\ndef main(argv=None):\n parser=argparse.ArgumentParser(description=__doc__);parser.add_argument(\'--root\',type=pathlib.Path,default=pathlib.Path(__file__).resolve().parents[3]);parser.add_argument(\'--schema\',type=pathlib.Path);parser.add_argument(\'--check\',action=\'store_true\');args=parser.parse_args(argv)\n root=args.root.resolve();source=args.schema or root/\'spec/production-system/integration/contracts/v1/schema-definitions.json\'\n try:\n  data=source.read_bytes();doc=json.loads(data)\n  if not isinstance(doc.get(\'$defs\'),dict) or not doc[\'$defs\']:raise GenerationError(\'nonempty $defs required\')\n  for name,node in doc[\'$defs\'].items():validate_schema(node,doc[\'$defs\'],name)\n  outputs=Generator(doc).emit();manifest={\'formatVersion\':1,\'generator\':\'tools/production/interfaces/codegen.py\',\'generatorSha256\':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),\'validatorTemplateSha256\':hashlib.sha256(pathlib.Path(__file__).with_name(\'validator.go.txt\').read_bytes()).hexdigest(),\'schemaPath\':str(source.relative_to(root)) if source.is_relative_to(root) else str(source),\'schemaSha256\':hashlib.sha256(data).hexdigest(),\'definitionCount\':len(doc[\'$defs\']),\'outputs\':{p:hashlib.sha256(v.encode()).hexdigest() for p,v in outputs.items()}}\n  outputs[\'spec/production-system/integration/contracts/v1/codegen-manifest.json\']=json.dumps(manifest,sort_keys=True,indent=2)+\'\\n\';stale=[]\n  for path,content in outputs.items():\n   target=root/path\n   if args.check:\n    if not target.is_file() or target.read_text()!=content:stale.append(path)\n   else:target.parent.mkdir(parents=True,exist_ok=True);target.write_text(content)\n  if stale:raise GenerationError(\'stale/missing generated outputs: \'+\', \'.join(stale))\n  print(json.dumps({\'status\':\'checked\' if args.check else \'generated\',\'definitions\':len(doc[\'$defs\']),\'outputs\':list(outputs)},sort_keys=True));return 0\n except (GenerationError,OSError,ValueError,KeyError) as e:print(\'codegen: \'+str(e),file=sys.stderr);return 1\nif __name__==\'__main__\':sys.exit(main())\n'

def frozen_v1_generator():
 if hashlib.sha256(FROZEN_V1_GENERATOR_SOURCE).hexdigest()!='9c86c1072975321e426a91d65dfd10123a3b9df6ca53c2a6dcc34a547abe3681':raise GenerationError("immutable v1 generator source mismatch")
 return FROZEN_V1_GENERATOR_SOURCE

KEYWORDS={'$schema','$id','$ref','$defs','type','title','description','default','examples','example','deprecated','readOnly','writeOnly','const','enum','oneOf','anyOf','allOf','not','properties','required','additionalProperties','items','minItems','maxItems','uniqueItems','minLength','maxLength','pattern','format','minimum','maximum','exclusiveMinimum','exclusiveMaximum','multipleOf','minProperties','maxProperties'}
FORMATS={'date-time','uuid','uri','email','hostname','ipv4','ipv6'}
def ident(s):
 value=''.join(p[:1].upper()+p[1:] for p in re.split('[^a-zA-Z0-9]',s) if p)
 if not value or value[0].isdigit():value='N'+value
 return value

def validate_schema(node,defs,path):
 if not isinstance(node,dict):raise GenerationError(f'{path}: boolean/nonobject schema unsupported; define a closed concrete schema')
 for k in node:
  if k not in KEYWORDS and not k.startswith('x-'):raise GenerationError(f'{path}: unsupported schema keyword {k}')
 if '$ref' in node:
  ref=node['$ref']
  if not ref.startswith('#/$defs/') or ref[8:] not in defs:raise GenerationError(f'{path}: unresolved/nonlocal ref {ref}')
 if node.get('format') and node['format'] not in FORMATS:raise GenerationError(f'{path}: unsupported format')
 if node.get('type')=='object' and node.get('additionalProperties') is not False and not node.get('properties'):
  if not isinstance(node.get('additionalProperties'),dict) or not isinstance(node.get('maxProperties'),int) or node['maxProperties']<1:
   raise GenerationError(f'{path}: open generic object requires explicit typed properties or bounded dictionary schema')
 if node.get('type')=='array' and not isinstance(node.get('items'),dict):raise GenerationError(f'{path}: typed array items required')
 if 'pattern' in node:
  # Generated Go uses RE2; incompatible regex must not silently change semantics.
  if re.search(r'\(\?[=!<]|\\[1-9]',node['pattern']):raise GenerationError(f'{path}: pattern not portable to RE2')
 for k in ('oneOf','anyOf','allOf'):
  for i,sub in enumerate(node.get(k,[])):validate_schema(sub,defs,f'{path}/{k}/{i}')
 if 'not' in node:validate_schema(node['not'],defs,path+'/not')
 if len({ident(k) for k in node.get('properties',{})})!=len(node.get('properties',{})):raise GenerationError(f'{path}: Go property identifier collision')
 for k,sub in node.get('properties',{}).items():validate_schema(sub,defs,path+'/'+k)
 if 'items' in node:validate_schema(node['items'],defs,path+'/items')
 if isinstance(node.get('additionalProperties'),dict):validate_schema(node['additionalProperties'],defs,path+'/additionalProperties')

def nullable(node):
 typ=node.get('type')
 if isinstance(typ,list) and 'null' in typ:
  rest=[t for t in typ if t!='null'];return dict(node,type=rest[0] if len(rest)==1 else rest)
 for key in ('anyOf','oneOf'):
  if key in node:
   arr=node[key];remaining=[n for n in arr if n.get('type')!='null']
   if len(remaining)!=len(arr):return remaining[0] if len(remaining)==1 else {key:remaining}
 return None

class Generator:
 def __init__(self,document,major=1,go_package=None,go_out=None,ts_out=None,format_output=True):
  if type(major) is not int or major<1:raise GenerationError('positive schema major required')
  self.major=major;self.go_package=go_package or 'v'+str(major)
  if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*',self.go_package):raise GenerationError('invalid Go package identity')
  self.go_out=go_out or 'sdk/launchpad/shared/interfaces/v'+str(major)
  self.ts_out=ts_out or ('sdk/launchpad/packages/production-contracts/src' if major==1 else 'sdk/launchpad/packages/production-contracts-v'+str(major)+'/src')
  for path,prefix in ((self.go_out,'sdk/launchpad/shared/interfaces/'),(self.ts_out,'sdk/launchpad/packages/')):
   candidate=pathlib.PurePosixPath(path)
   if candidate.is_absolute() or '..' in candidate.parts or not path.startswith(prefix):raise GenerationError('output path outside generated allowlist')
  if major!=1 and (self.go_out=='sdk/launchpad/shared/interfaces/v1' or self.ts_out=='sdk/launchpad/packages/production-contracts/src'):raise GenerationError('versioned output cannot overwrite v1')
  self.format_output=format_output;self.document=document;self.nodes=dict(document['$defs']);self.output={};self.unions={};self.used=set(self.nodes)
 def lift(self,node,hint):
  name=hint;counter=2
  while name in self.used:
   if self.nodes.get(name)==node:return name
   name=hint+str(counter);counter+=1
  self.used.add(name);self.nodes[name]=node;return name
 def type(self,node,hint,lang):
  n=nullable(node)
  if n is not None:
   inner=self.type(n,hint+'NonNull',lang);return '*'+inner if lang=='go' else inner+' | null'
  if '$ref' in node:return node['$ref'][8:]
  if any(k in node for k in ('oneOf','anyOf')):
   name=self.lift(node,hint);return name
  if 'allOf' in node:raise GenerationError('allOf types require materialized flattening before generation')
  typ=node.get('type')
  if not typ:
   if 'const' in node:typ={str:'string',bool:'boolean',int:'integer',type(None):'null'}.get(type(node['const']))
   elif node.get('enum'):typ=type(node['enum'][0]);typ={str:'string',int:'integer',bool:'boolean'}.get(typ)
  if isinstance(typ,list):raise GenerationError(f'{hint}: nonnullable multi-type must be explicit variants')
  if typ=='object':
   if isinstance(node.get('additionalProperties'),dict) and not node.get('properties'):
    value=self.type(node['additionalProperties'],hint+'Value',lang)
    return 'map[string]'+value if lang=='go' else 'Record<string, '+value+'>'
   if lang=='ts':return '{ '+ '; '.join(json.dumps(k)+('' if k in node.get('required',[]) else '?')+': '+self.type(v,hint+ident(k),'ts') for k,v in node.get('properties',{}).items())+' }'
   return 'struct {\n'+''.join('\t'+ident(k)+' '+(self.type(v,hint+ident(k),'go') if k in node.get('required',[]) else 'Optional['+self.type(v,hint+ident(k),'go')+']')+' `json:'+json.dumps(k+('' if k in node.get('required',[]) else ',omitzero'))+'`\n' for k,v in node.get('properties',{}).items())+'}'
  if typ=='array':return ('[]'+self.type(node['items'],hint+'Item','go')) if lang=='go' else 'Array<'+self.type(node['items'],hint+'Item','ts')+'>'
  if lang=='ts' and ('enum' in node or 'const' in node):return ' | '.join(json.dumps(v) for v in node.get('enum',[node.get('const')]))
  values={'string':('string','string'),'integer':('SafeInteger','number'),'number':('float64','number'),'boolean':('bool','boolean'),'null':('NullValue','null')}
  if typ not in values:raise GenerationError(f'{hint}: no concrete type for {node}')
  return values[typ][lang=='ts']
 def emit(self):
  # Iteration admits lifted nested unions while preserving deterministic insertion order.
  go=[];ts=[];done=set()
  while len(done)<len(self.nodes):
   name=next(n for n in self.nodes if n not in done);done.add(name);node=self.nodes[name]
   if not re.fullmatch('[A-Z][A-Za-z0-9]*',name):raise GenerationError(f'invalid definition identifier {name}')
   branch_key=next((k for k in ('oneOf','anyOf') if k in node),None)
   nonnull=nullable(node)
   if branch_key and nonnull is None:
    branches=node[branch_key];types=[self.type(v,name+'Variant'+str(i),'go') for i,v in enumerate(branches,1)]
    go.append('type '+name+' struct {\n'+''.join(f'\tVariant{i} *{t} `json:"-"`\n' for i,t in enumerate(types,1))+'}\n')
    rawschema=json.dumps(json.dumps(node,separators=(',',':')))
    go.append(f'func (v *{name}) UnmarshalJSON(raw []byte) error {{\n if err:=validateNode(json.RawMessage({rawschema}),raw,"$");err!=nil {{return err}}\n *v={name}{{}}\n')
    for i,t in enumerate(types,1):
     branch=json.dumps(json.dumps(branches[i-1],separators=(',',':')))
     go.append(f' if validateNode(json.RawMessage({branch}),raw,"$")==nil {{var value {t};if err:=json.Unmarshal(raw,&value);err!=nil{{return err}};v.Variant{i}=&value;return nil}}\n')
    go.append('return fmt.Errorf("no union variant")\n}\n')
    go.append(f'func (v {name}) MarshalJSON() ([]byte,error) {{\n count:=0;var raw []byte;var err error\n')
    for i in range(1,len(types)+1):go.append(f' if v.Variant{i}!=nil{{count++;raw,err=json.Marshal(v.Variant{i});if err!=nil{{return nil,err}}}}\n')
    go.append('if count!=1{return nil,fmt.Errorf("union requires exactly one selected variant")};'+f'if err:=validateNode(json.RawMessage({rawschema}),raw,"$");err!=nil{{return nil,err}};return raw,nil\n}}\n')
    ts.append('export type '+name+' = '+' | '.join(self.type(v,name+'Variant'+str(i),'ts') for i,v in enumerate(branches,1))+';\n')
   else:
    go.append('type '+name+(' = ' if '$ref' in node or node.get('type') in ('null','integer') else ' ')+self.type(node,name,'go')+'\n');ts.append('export type '+name+' = '+self.type(node,name,'ts')+';\n')
  # The runtime schema bundle also includes lifted named variants.
  doc=dict(self.document);doc['$defs']=self.nodes
  schema=json.dumps(doc,sort_keys=True,indent=2)+'\n';digest=hashlib.sha256(schema.encode()).hexdigest()
  go_header='// Code generated by tools/production/interfaces/codegen.py; DO NOT EDIT.\n// Schema SHA256: '+digest+'\npackage '+self.go_package+'\n\nimport ("bytes";"encoding/json";"fmt")\n\ntype NullValue struct{}\nfunc (NullValue) MarshalJSON()([]byte,error){return []byte("null"),nil}\nfunc (*NullValue) UnmarshalJSON(raw []byte)error{if string(bytes.TrimSpace(raw))!="null"{return fmt.Errorf("expected null")};return nil}\n'
  # Keep imports used even if a tiny synthetic schema has no union.
  go_header+='var _ = json.Valid\nvar _ = fmt.Errorf\n'
  go_header+='type Optional[T any] struct { Present bool; Value T }\nfunc (v Optional[T]) IsZero()bool{return !v.Present}\nfunc (v Optional[T]) MarshalJSON()([]byte,error){if !v.Present{return nil,fmt.Errorf("absent optional value cannot be serialized directly")};return json.Marshal(v.Value)}\nfunc (v *Optional[T]) UnmarshalJSON(raw []byte)error{var value T;if err:=json.Unmarshal(raw,&value);err!=nil{return err};v.Value=value;v.Present=true;return nil}\n'
  go_header+='type SafeInteger int64\nfunc (v SafeInteger) MarshalJSON()([]byte,error){if v < -9007199254740991 || v > 9007199254740991{return nil,fmt.Errorf("integer outside JS safe range")};return json.Marshal(int64(v))}\nfunc (v *SafeInteger) UnmarshalJSON(raw []byte)error{r,err:=rational(raw);if err!=nil{return err};if !r.IsInt()||!r.Num().IsInt64(){return fmt.Errorf("integer out of range")};n:=r.Num().Int64();if n < -9007199254740991 || n > 9007199254740991{return fmt.Errorf("integer outside JS safe range")};*v=SafeInteger(n);return nil}\n'
  decoders=[]
  for name in sorted(self.document['$defs']):decoders.append(f'func Decode{name}(raw []byte) ({name},error) {{var value {name};if err:=ValidateJSON("{name}",raw);err!=nil{{return value,err}};err:=json.Unmarshal(raw,&value);return value,err}}\n')
  gocode=go_header+''.join(go)+''.join(decoders)
  go_output=self.format_go(gocode)
  validators=(pathlib.Path(__file__).parent/'validator.go.txt').read_text().replace('__SCHEMA_JSON__',json.dumps(schema))
  if self.major!=1 or self.go_package!='v1':
   lines=validators.splitlines(keepends=True);packages=0;versions=0
   for i,line in enumerate(lines):
    if line=='package v1\n':lines[i]='package '+self.go_package+'\n';packages+=1
    if 'if schemaMajor!=1{' in line:
     lines[i]=line.replace('if schemaMajor!=1{','if schemaMajor!='+str(self.major)+'{',1);versions+=1
   if packages!=1 or versions!=1:raise GenerationError('validator package/version template contract changed')
   validators=''.join(lines)
  validator_output=self.format_go(validators)
  ts_header='// Code generated; DO NOT EDIT. Schema SHA256: '+digest+'\n'
  ts.append('export interface ContractTypes {\n'+''.join(json.dumps(n)+': '+n+';\n' for n in sorted(self.document['$defs']))+'}\n')
  return {self.go_out+'/types.gen.go':go_output,self.go_out+'/validators.gen.go':validator_output,self.ts_out+'/types.gen.ts':ts_header+''.join(ts),self.ts_out+'/schemas.gen.json':schema}
 def format_go(self,source):
  if not self.format_output:return source
  result=subprocess.run(['gofmt'],input=source,text=True,capture_output=True)
  if result.returncode:raise GenerationError('gofmt: '+result.stderr)
  return result.stdout

def main(argv=None):
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--root',type=pathlib.Path,default=pathlib.Path(__file__).resolve().parents[4]);parser.add_argument('--schema',type=pathlib.Path);parser.add_argument('--check',action='store_true');parser.add_argument('--major',type=int,default=1);parser.add_argument('--go-package');parser.add_argument('--go-out');parser.add_argument('--ts-out');parser.add_argument('--manifest',type=pathlib.Path);parser.add_argument('--no-format',action='store_true');args=parser.parse_args(argv)
 root=args.root.resolve();source=args.schema or root/('launchpad/spec/production-system/integration/contracts/v'+str(args.major)+'/schema-definitions.json')
 try:
  data=source.read_bytes();doc=json.loads(data)
  if not isinstance(doc.get('$defs'),dict) or not doc['$defs']:raise GenerationError('nonempty $defs required')
  for name,node in doc['$defs'].items():validate_schema(node,doc['$defs'],name)
  outputs=Generator(doc,args.major,args.go_package,args.go_out,args.ts_out,not args.no_format).emit();manifest={'formatVersion':1,'generator':'tools/production/interfaces/codegen.py','generatorSha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'validatorTemplateSha256':hashlib.sha256(pathlib.Path(__file__).with_name('validator.go.txt').read_bytes()).hexdigest(),'schemaPath':str(source.relative_to(root)) if source.is_relative_to(root) else str(source),'schemaSha256':hashlib.sha256(data).hexdigest(),'definitionCount':len(doc['$defs']),'outputs':{p:hashlib.sha256(v.encode()).hexdigest() for p,v in outputs.items()}}
  manifest_path=args.manifest or pathlib.Path('launchpad/spec/production-system/integration/contracts/v'+str(args.major)+'/codegen-manifest.json')
  if manifest_path.is_absolute():
   if not manifest_path.is_relative_to(root):raise GenerationError('manifest outside source root')
   manifest_path=manifest_path.relative_to(root)
  if '..' in manifest_path.parts or not str(manifest_path).startswith('launchpad/spec/production-system/integration/contracts/'):raise GenerationError('manifest outside contract allowlist')
  if args.major==1:
   frozen=json.loads((root/manifest_path).read_bytes())
   if frozen['schemaSha256']!=manifest['schemaSha256'] or frozen['outputs']!=manifest['outputs']:raise GenerationError('immutable v1 input/output reproduction differs')
   manifest=frozen
  else:manifest.update(schemaMajor=args.major,goPackage=args.go_package or 'v'+str(args.major),formatOutput=not args.no_format)
  outputs[str(manifest_path)]=json.dumps(manifest,sort_keys=True,indent=2)+'\n';stale=[]
  for path,content in outputs.items():
   target=root/path
   if args.check:
    if not target.is_file() or target.read_text()!=content:stale.append(path)
   else:target.parent.mkdir(parents=True,exist_ok=True);target.write_text(content)
  if stale:raise GenerationError('stale/missing generated outputs: '+', '.join(stale))
  print(json.dumps({'status':'checked' if args.check else 'generated','definitions':len(doc['$defs']),'outputs':list(outputs)},sort_keys=True));return 0
 except (GenerationError,OSError,ValueError,KeyError) as e:print('codegen: '+str(e),file=sys.stderr);return 1
if __name__=='__main__':sys.exit(main())
