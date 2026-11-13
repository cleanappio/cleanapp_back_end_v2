#!/usr/bin/env python3
"""Explicit AWS-only native Docker restart fence/restore; never recreate/start/stop.
Default is a private-snapshot plan with no Docker calls. During isolation every
captured container must have restart=no, independent of systemd ACTIVE guards.
"""
import argparse,collections,json,os,stat,subprocess,sys
from pathlib import Path
MARKER=Path('/etc/cleanapp-runtime/ISOLATED')
def private_json(path):
 fd=os.open(path,os.O_RDONLY|getattr(os,'O_NOFOLLOW',0))
 with os.fdopen(fd) as stream:
  value=os.fstat(stream.fileno())
  if not stat.S_ISREG(value.st_mode) or value.st_mode&0o077:raise ValueError('Private snapshot required')
  return json.load(stream)
def run(args):
 r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 if r.returncode:raise RuntimeError('Native restart command failed; raw output suppressed')
 return r.stdout

def policy(original):
 name=original.get('Name') or 'no';count=original.get('MaximumRetryCount',0)
 if name not in ['no','always','unless-stopped','on-failure']:raise ValueError('Unsupported original restart policy')
 return name+(':'+str(count) if name=='on-failure' and count else '')
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['fence','restore','verify-isolated','verify-restored']);p.add_argument('--snapshot',required=True);p.add_argument('--execute',action='store_true');args=p.parse_args()
 snapshot=private_json(args.snapshot);containers=snapshot['containers']
 library='/usr/local/lib/cleanapp-backup'
 if library not in sys.path:sys.path.insert(0,library)
 from runtime_roles import declared_roles,snapshot_roles
 baseline=private_json('/opt/cleanapp/private/compose-target.json')
 snapshot_roles(snapshot,declared_roles(baseline).values())
 expected={c['Name'].lstrip('/'):c['HostConfig'].get('RestartPolicy',{}) for c in containers}
 if not args.execute:print(json.dumps({'plan':True,'action':args.action,'containers':len(expected),'original_policy_counts':dict(collections.Counter(policy(v) for v in expected.values())),'Docker_calls':0,'recreates':0,'starts':0,'stops':0}));return
 vendor=Path('/sys/devices/virtual/dmi/id/sys_vendor').read_text().strip()
 if 'Amazon' not in vendor:raise ValueError('Refusing native restart mutations outside AWS')
 inspected={name:json.loads(run(['docker','inspect','--format','{{json .HostConfig.RestartPolicy}}',name])) for name in expected}
 if args.action=='fence':
  MARKER.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
  if MARKER.is_symlink():raise ValueError('Isolation marker must not be a symlink')
  fd=os.open(MARKER,os.O_WRONLY|os.O_CREAT|getattr(os,'O_NOFOLLOW',0),0o600)
  with os.fdopen(fd,'w') as stream:stream.write('Native restart policies fenced during isolated migration validation.\n');stream.flush();os.fsync(stream.fileno())
  run(['docker','update','--restart=no',*expected])
 elif args.action=='restore':
  # Restore only when all are stopped, after original cold reset and sole-writer decision.
  if not MARKER.is_file():raise ValueError('Isolation marker missing; explicit activation review required')
  for name in expected:
   state=json.loads(run(['docker','inspect','--format','{{json .State.Running}}',name]))
   if state:raise ValueError('Stop all containers before final native policy restoration')
  for name,value in expected.items():run(['docker','update','--restart='+policy(value),name])
 isolated=args.action in ['fence','verify-isolated']
 mismatches=0
 for name,value in expected.items():
  actual=json.loads(run(['docker','inspect','--format','{{json .HostConfig.RestartPolicy}}',name]))
  wanted={'Name':'no','MaximumRetryCount':0} if isolated else value
  if actual!=wanted:mismatches+=1
 print(json.dumps({'containers':len(expected),'restart_policy_mismatches':mismatches,'isolated':isolated,'ISOLATED_marker_retained':MARKER.exists(),'recreates':0,'starts':0,'stops':0}))
 if mismatches:sys.exit(1)
if __name__=='__main__':
 try:main()
 except Exception as error:print(json.dumps({'native_restart_helper_failed':True,'error_type':type(error).__name__}),file=sys.stderr);sys.exit(2)
