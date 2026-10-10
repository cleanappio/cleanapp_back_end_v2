"""Replace only the four Bluesky workers, preserving runtime credentials/settings.
Run as root on the VM. Private snapshots and renamed containers provide rollback.
"""
import json, os, pathlib, subprocess, sys, time
import yaml

# Compose's !reset/!override tags must survive a YAML round trip.
class Tagged:
    def __init__(self, tag, value): self.tag, self.value = tag, value
class ComposeLoader(yaml.SafeLoader): pass
class ComposeDumper(yaml.SafeDumper): pass
def load_tag(loader, suffix, node):
    if isinstance(node, yaml.SequenceNode): value=loader.construct_sequence(node,deep=True)
    elif isinstance(node, yaml.MappingNode): value=loader.construct_mapping(node,deep=True)
    else: value=loader.construct_scalar(node)
    return Tagged('!'+suffix,value)
def dump_tag(dumper, value):
    node=dumper.represent_data(value.value); node.tag=value.tag; return node
ComposeLoader.add_multi_constructor('!',load_tag)
ComposeDumper.add_representer(Tagged,dump_tag)

IMAGE = sys.argv[1]
STAMP = '20261009-diverse'
ROOT = pathlib.Path('/home/deployer/bluesky-rollouts') / STAMP
ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
os.chmod(ROOT, 0o700)
BINARIES = {'cleanapp_bluesky_indexer':'index_bluesky',
            'cleanapp_bluesky_now':'bluesky_now',
            'cleanapp_bluesky_analyzer':'analyzer_bluesky',
            'cleanapp_bluesky_submitter':'submitter_bluesky'}
QUERIES = 'pothole,illegal dumping,blocked sidewalk,broken streetlight,water leak,fallen tree,broken link,404 error,app crashing,login broken,sync failed,accessibility issue,confusing interface,unusable app'

def call(args):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if p.returncode:
        # Do not echo commands, environment, or unredacted Docker diagnostics.
        raise RuntimeError(f'{args[0]} {args[1]} failed (exit {p.returncode})')
    return p.stdout

call(['docker','pull',IMAGE])
specs = json.loads(call(['docker','inspect',*BINARIES]))
snapshot=ROOT/'before.json'
if snapshot.exists():
    saved=json.loads(snapshot.read_text())
    if {x['Id'] for x in saved}!={x['Id'] for x in specs}:
        raise RuntimeError('Rollback snapshot belongs to different containers')
else:
    snapshot.write_text(json.dumps(specs)); os.chmod(snapshot,0o600)
override=pathlib.Path('/home/deployer/docker-compose.override.yml')
backup=ROOT/'docker-compose.override.yml'
if not backup.exists(): backup.write_bytes(override.read_bytes()); os.chmod(backup,0o600)
config=yaml.load(override.read_text(),Loader=ComposeLoader) or {}
services=config.setdefault('services',{})
for spec in specs:
    name=spec['Name'].lstrip('/')
    service=services.setdefault(name,{})
    service['image']=IMAGE
    service['entrypoint']=['/usr/local/bin/'+BINARIES[name]]
    command=spec['Config']['Cmd'] or []
    if name=='cleanapp_bluesky_now': command=command[1:]
    service['command']=command
    if name=='cleanapp_bluesky_indexer':
        environment=service.get('environment',{})
        if isinstance(environment,list): environment=dict(x.split('=',1) for x in environment)
        environment.update(BSKY_SEARCH_QUERIES=QUERIES,BSKY_PAGES_PER_RUN='1')
        service['environment']=environment

changed=[]
try:
    # Prevent the old submitter from consuming new collector output mid-rollout.
    for name in ['cleanapp_bluesky_submitter','cleanapp_bluesky_analyzer',
                 'cleanapp_bluesky_indexer','cleanapp_bluesky_now']:
        call(['docker','stop','--time','15',name])
    for spec in specs:
        name=spec['Name'].lstrip('/')
        old=name+'-before-'+STAMP
        call(['docker','rename',name,old]); changed.append((name,old))
        env=dict(x.split('=',1) for x in spec['Config']['Env'])
        if name=='cleanapp_bluesky_indexer': env.update(BSKY_SEARCH_QUERIES=QUERIES,BSKY_PAGES_PER_RUN='1')
        envfile=ROOT/(name+'.env')
        envfile.write_text('\n'.join(k+'='+v for k,v in env.items())+'\n');os.chmod(envfile,0o600)
        networks=list(spec['NetworkSettings']['Networks'])
        if len(networks)!=1: raise RuntimeError('Unexpected worker network configuration')
        args=['docker','run','-d','--name',name,'--network',networks[0],
              '--network-alias',name,'--restart',spec['HostConfig']['RestartPolicy']['Name'],
              '--env-file',str(envfile),'--entrypoint','/usr/local/bin/'+BINARIES[name]]
        if spec['Config'].get('WorkingDir'): args+=['--workdir',spec['Config']['WorkingDir']]
        for mount in spec['Mounts']:
            if mount['Type']!='bind': raise RuntimeError('Unexpected worker storage type')
            args+=['--volume',mount['Source']+':'+mount['Destination']+('' if mount['RW'] else ':ro')]
        for key,value in spec['Config'].get('Labels',{}).items(): args+=['--label',key+'='+value]
        command=spec['Config']['Cmd'] or []
        if name=='cleanapp_bluesky_now': command=command[1:]
        call(args+[IMAGE]+command)
        envfile.unlink()
    time.sleep(8)
    after=json.loads(call(['docker','inspect',*BINARIES]))
    if any(not x['State']['Running'] or x['RestartCount'] for x in after):
        raise RuntimeError('A worker failed startup')
    override.write_text(yaml.dump(config,Dumper=ComposeDumper,sort_keys=False))
    print('Rollout complete: four Bluesky workers running; previous containers retained for rollback.')
except Exception:
    for name,old in reversed(changed):
        subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        call(['docker','rename',old,name])
    for name in BINARIES: call(['docker','start',name])
    override.write_bytes(backup.read_bytes())
    raise
