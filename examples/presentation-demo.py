import json,subprocess
p=subprocess.run(['go','run','./cmd/tokensched','run','examples/overrun-tasktree.yaml','--budget','200k','--json'],capture_output=True,text=True,check=True)
x=json.loads(p.stdout)
print(json.dumps({'budget':x['budget'],'overrun_tokens':x['overrun_tokens'],'naive':x['naive'],'scheduled':x['scheduled'],'tasks_saved':x['tasks_saved']},indent=2))
