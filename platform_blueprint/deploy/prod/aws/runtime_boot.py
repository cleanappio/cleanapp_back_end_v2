#!/usr/bin/env python3
"""Start/stop only exact captured baseline services, retaining BOTH existing CI pins.
Default is a local-file plan. No source Compose, image pulls, builds, cloud API,
volume/network creation or raw command output. Docker restart policies persist.
"""
import argparse,contextlib,json,os,stat,subprocess,sys,time
from pathlib import Path
BASE=Path('/opt/cleanapp/private/compose-target.json')
DEPLOY=Path('/opt/cleanapp/deploy')
PINS=[DEPLOY/'docker-compose.aws-backend-images.yml',DEPLOY/'docker-compose.aws-frontend-images.yml']
BACKUP_LIBRARY=Path('/usr/local/lib/cleanapp-backup')
def run(args):
 r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 if r.returncode:raise RuntimeError('Runtime command failed; diagnostics suppressed')
 return r.stdout

def configuration():
 fd=os.open(BASE,os.O_RDONLY|getattr(os,'O_NOFOLLOW',0))
 with os.fdopen(fd) as stream:
  mode=os.fstat(stream.fileno())
  if not stat.S_ISREG(mode.st_mode) or mode.st_mode&0o077:raise ValueError('Baseline must be a private regular file')
  baseline=json.load(stream)
 if str(BACKUP_LIBRARY) not in sys.path:sys.path.insert(0,str(BACKUP_LIBRARY))
 from runtime_roles import declared_roles
 declared_roles(baseline)
 services=baseline['services'];names=sorted(services)
 db=[key for key,v in services.items() if v.get('container_name')=='cleanapp_db'];broker=[key for key,v in services.items() if v.get('container_name')=='cleanapp_rabbitmq']
 if len(db)!=1 or len(broker)!=1:raise ValueError('Database/broker identity missing')
 command=['docker','compose','--project-directory',str(DEPLOY),'--project-name',baseline['name'],'-f',str(BASE)]
 present=[]
 for path in PINS:
  if path.is_symlink():raise ValueError('CI pin file must not be a symlink')
  if path.exists():command+=['-f',str(path)];present.append(path)
 return names,db,broker,command,present

def load_shared_lock():
 # Load the reviewed helper only for an executed start/stop, never for a plan.
 if str(BACKUP_LIBRARY) not in sys.path:sys.path.insert(0,str(BACKUP_LIBRARY))
 from backup_recovery import shared_deployment_lock
 return shared_deployment_lock

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--action',choices=['start','stop','plan'],default='plan');p.add_argument('--execute',action='store_true');args=p.parse_args()
 if not args.execute or args.action=='plan':
  names,db,broker,command,present=configuration()
  print(json.dumps({'plan':True,'baseline_services':len(names),'existing_ci_pin_files':len(present),'pulls':0,'builds':0}));return
 shared_deployment_lock=load_shared_lock()
 with contextlib.ExitStack() as stack:
  while True:
   try:
    stack.enter_context(shared_deployment_lock(DEPLOY/'.deployment-and-backup.lock'))
    break
   except BlockingIOError:time.sleep(1)
  # Read baseline/pins after acquiring the lock: CI may have updated a pin
  # while this service was waiting. Never retry operations inside the body.
  names,db,broker,command,present=configuration()
  resolved=json.loads(run(command+['config','--format','json']))
  if set(resolved['services'])!=set(names):raise ValueError('CI pins may not add/remove captured baseline services')
  if args.action=='start':
   run(command+['up','-d','--no-build','--pull','never','--wait','--wait-timeout','300',*db,*broker])
   run(command+['up','-d','--no-build','--pull','never',*names])
  else:
   apps=[key for key in names if key not in db+broker]
   run(command+['stop','--timeout','90',*apps])
   run(command+['stop','--timeout','120',*broker])
   run(command+['stop','--timeout','900',*db])
 print(json.dumps({'action':args.action,'baseline_services':len(names),'ci_pin_files_used':len(present),'pulls':0,'builds':0,'raw_output_printed':False}))
if __name__=='__main__':
 try:main()
 except Exception as error:print(json.dumps({'runtime_unit_failed':True,'error_type':type(error).__name__}),file=sys.stderr);sys.exit(1)
