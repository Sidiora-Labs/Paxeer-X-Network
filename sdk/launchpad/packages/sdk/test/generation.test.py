"""Run pinned generator twice in clean private roots, compare all SDK output bytes."""
import hashlib,json,pathlib,shutil,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[4]
paths=['client/packages/sdk/scripts/generate.py','tools/production/interfaces/codegen.py','tools/production/interfaces/validator.go.txt']
contracts='spec/production-system/integration/contracts/v1/'
paths += [contracts+n for n in ['codegen-manifest.json','schema-definitions.json','openapi.json','asyncapi.json']]
outputs=['src/types.gen.ts','src/schemas.gen.json','release-manifest.json']
with tempfile.TemporaryDirectory(prefix='L24-codegen-') as temporary:
 result=[]
 for name in ['clean-a','clean-b']:
  private=pathlib.Path(temporary)/name
  for path in paths:
   target=private/path;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(root/path,target)
  (private/'client/packages/sdk/src').mkdir()
  subprocess.run(['python3',str(private/'client/packages/sdk/scripts/generate.py'),'--reproduce-v1'],check=True,capture_output=True)
  result.append({n:(private/'client/packages/sdk'/n).read_bytes() for n in outputs})
 assert result[0]==result[1],'clean generations differ'
 for path,data in result[0].items():assert (root/'client/packages/sdk'/path).read_bytes()==data,'checked-in output stale: '+path
 print(json.dumps({'cleanGenerations':2,'identical':True,'outputs':{n:hashlib.sha256(b).hexdigest() for n,b in result[0].items()}},sort_keys=True))

# Additive v2 operations and executable examples; legacy output assertions above remain intact.
v2contracts='spec/production-system/integration/contracts/v2/'
with tempfile.TemporaryDirectory(prefix='L24-v2-codegen-') as temporary:
 generated=[]
 for name in ['clean-a','clean-b']:
  private=pathlib.Path(temporary)/name
  for path in ['client/packages/sdk/scripts/generate.py','client/packages/sdk/scripts/examples.py']+[v2contracts+n for n in ['openapi.json','asyncapi.json','schema-definitions.json','codegen-manifest.json']]:
   target=private/path;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(root/path,target)
  (private/'client/packages/sdk/src').mkdir(parents=True)
  subprocess.run(['python3',str(private/'client/packages/sdk/scripts/generate.py'),'--major','2','--operations-only'],check=True,capture_output=True)
  subprocess.run(['python3',str(private/'client/packages/sdk/scripts/examples.py')],check=True,capture_output=True)
  names=['src/operations.gen.ts','examples/pagination.mjs','examples/snapshot-resume.mjs']
  generated.append({path:(private/'client/packages/sdk'/path).read_bytes() for path in names})
 assert generated[0]==generated[1], 'versioned clean generations differ'
 for path,data in generated[0].items(): assert (root/'client/packages/sdk'/path).read_bytes()==data, 'versioned generated output stale: '+path
 print(json.dumps({'version':2,'cleanGenerations':2,'identical':True,'outputs':{name:hashlib.sha256(data).hexdigest() for name,data in generated[0].items()}},sort_keys=True))
