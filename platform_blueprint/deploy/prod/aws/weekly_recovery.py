#!/usr/bin/env python3
"""Verified weekly DB + CURRENT image/config recovery set. Dry-run unless --execute.
Separate wrapper: cold/restore helper interfaces are unchanged. One outer deploy
lock serializes all captures, artifact readbacks and DB snapshot creation.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile
import tempfile

import backup_recovery as b
import runtime_migration as runtime
from runtime_roles import declared_roles, snapshot_roles, capture_declared_runtime

BASELINE='/opt/cleanapp/private/compose-target.json'
PINS=['/opt/cleanapp/deploy/docker-compose.aws-backend-images.yml','/opt/cleanapp/deploy/docker-compose.aws-frontend-images.yml']
LOCK='/opt/cleanapp/deploy/.deployment-and-backup.lock'
FORBIDDEN=[LOCK,'/etc/cleanapp-runtime/ACTIVE','/etc/cleanapp-runtime/ISOLATED','/etc/cleanapp-scheduler/activation']
MAX_CONFIG_BYTES=256*1024**2


def validate_config_path(value):
    b.require(isinstance(value,str) and value.startswith('/') and value!='/' and value==os.path.normpath(value) and '..' not in Path(value).parts,'Invalid reviewed config path')
    for forbidden in FORBIDDEN:
        b.require(value!=forbidden and not forbidden.startswith(value+'/') and not value.startswith(forbidden+'/'),'Config scope must exclude activation markers and the shared lock')
    b.require(not any(value==p or value.startswith(p+'/') for p in ['/proc','/sys','/dev','/run','/var/lib/docker/volumes']),'Large/runtime state is not a small configuration artifact')
    return value


def normalize_paths(values):
    values=sorted(set(validate_config_path(value) for value in values))
    return [value for value in values if not any(value!=parent and value.startswith(parent+'/') for parent in values)]


def small_size(roots):
    stack=[Path(root) for root in roots];total=0
    while stack:
        path=stack.pop();info=path.lstat()
        if path.is_symlink():continue
        if path.is_dir():
            with os.scandir(path) as items:stack.extend(Path(item.path) for item in items)
        elif path.is_file():total+=info.st_size
        b.require(total<=MAX_CONFIG_BYTES,'Reviewed configuration grew beyond 256MiB; do not upload data/log trees as config')
    return total


def capture_current(folder,expected_count):
    baseline=b.load_json(BASELINE);roles=declared_roles(baseline)
    b.require(expected_count is None or expected_count==len(roles),'Expected count differs from the declared production role set')
    snapshot=capture_declared_runtime(roles.values())
    runtime.save_private(folder/'runtime-current.private.json',snapshot)
    runtime.save_private(folder/'compose-current.private.json',runtime.reconstruct(snapshot))
    return snapshot


def check_runtime_files(snapshot):
    baseline=b.load_json(BASELINE);services=baseline.get('services',{});roles=declared_roles(baseline)
    snapshot_roles(snapshot,roles.values(),require_running=True)
    by_name={value['Name'].lstrip('/'):value for value in snapshot['containers']}
    resolved={key:value['image'] for key,value in services.items()};present=[]
    for name in PINS:
        path=Path(name)
        if not path.exists():
            b.require(not path.is_symlink(),'Dangling CI pin symlink is invalid');continue
        pin=b.load_json(path);b.require(set(pin)=={'services'},'CI pin has unexpected top-level configuration')
        for service,value in pin['services'].items():
            b.require(service in services and set(value)<= {'image','pull_policy'} and value.get('pull_policy')=='never','CI pin changes unsupported service/runtime configuration')
            b.require(re.fullmatch(r'sha256:[0-9a-f]{64}',value.get('image','')),'CI pin must reference immutable image ID')
            resolved[service]=value['image']
        present.append(str(path))
    image_cache={}
    for service,image in resolved.items():
        if image not in image_cache:image_cache[image]=b.quiet_run(['docker','image','inspect','--format','{{.Id}}',image]).decode().strip()
        b.require(image_cache[image]==by_name[services[service]['container_name']]['Image'],'Baseline plus BOTH CI pins does not resolve to the actual current image; refuse stale recovery metadata')
    return [BASELINE,*present]


def capture_overlays(folder):
    target=folder/'overlays';target.mkdir(mode=0o700);index=[]
    for number,(container,path) in enumerate(runtime.OVERLAYS):
        output=target/(str(number)+'.private.tar')
        with runtime.private_file(output) as stream:
            result=subprocess.run(['docker','cp','-a',container+':'+path,'-'],stdout=stream,stderr=subprocess.PIPE,timeout=90)
        b.require(result.returncode==0,'Current small writable configuration capture failed')
        sha,size=b.digest_file(output);b.require(size<=16*1024**2,'Writable configuration overlay exceeds reviewed small scope')
        index.append({'container':container,'source_path':path,'archive':output.name,'sha256':sha})
    runtime.save_private(target/'index.private.json',index)


def config_paths(snapshot,reviewed,folder,runtime_files):
    config=b.load_json(reviewed);b.require(isinstance(config,dict) and set(config)=={'required','optional'},'Reviewed config scope needs required/optional arrays')
    paths=[*runtime_files,str(folder)]
    for name in config['required']:
        validate_config_path(name);b.require(os.path.lexists(name),'Required host recovery configuration missing');paths.append(name)
    for name in config['optional']:
        validate_config_path(name)
        if os.path.lexists(name):paths.append(name)
    for container in snapshot['containers']:
        for mount in container.get('Mounts',[]):
            if mount['Type']=='bind' and not mount.get('RW',True):
                name=validate_config_path(mount['Source']);b.require(os.path.lexists(name),'Current read-only config bind is missing');paths.append(name)
    roots=normalize_paths(paths);small_size(roots);return roots


def image_artifact(store,bid,snapshot):
    ids=sorted({value['Image'] for value in snapshot['containers']})
    for image in ids:runtime.image_tag(image)
    key='weekly/artifacts/'+bid+'/images.tar.gz'
    record=b.pipeline_upload(store,key,['docker','image','save',*ids],['gzip','-1','-c']);record['data_key']=key
    proof=verify_image_object(store,record,ids);receipt=b.verified_record('images',key,record,proof);receipt['image_ids']=ids
    marker='weekly/artifacts/'+bid+'/images.verified.json'
    # Small verified metadata lives beside its unique artifact; no shared/deduped object.
    receipt['manifest_key']=marker;store.put_json(marker,receipt);b.require(store.get_json(marker)==receipt,'Image artifact marker roundtrip failed')
    return receipt


def verify_image_object(store,record,expected_ids):
    """Independent full compressed readback, gzip CRC, tar and exact image configs."""
    remote=store.download(record['data_key'],record.get('version_id'));reader=b.HashReader(remote.stream)
    try:
        hashes={};manifest=None
        with gzip.GzipFile(fileobj=reader,mode='rb') as clear:
            with tarfile.open(fileobj=clear,mode='r|') as archive:
                for member in archive:
                    name=member.name.rstrip('/');parts=PurePosixPath(name)
                    b.require(name and not parts.is_absolute() and '..' not in parts.parts and name not in hashes,'Unsafe/duplicate Docker image archive entry')
                    b.require(member.isdir() or member.isfile(),'Unexpected link/special Docker image archive entry')
                    if member.isdir():continue
                    digest=hashlib.sha256();capture=bytearray() if name=='manifest.json' else None
                    with archive.extractfile(member) as stream:
                        while block:=stream.read(b.CHUNK):
                            digest.update(block)
                            if capture is not None:
                                capture.extend(block);b.require(len(capture)<=4*1024**2,'Image manifest unexpectedly large')
                    hashes[name]=digest.hexdigest()
                    if capture is not None:manifest=json.loads(capture)
            while clear.read(b.CHUNK):pass
        while reader.read(b.CHUNK):pass
        b.require(isinstance(manifest,list) and manifest,'Docker image manifest missing')
        configs=set()
        for image in manifest:
            config=image.get('Config');layers=image.get('Layers')
            b.require(config in hashes and isinstance(layers,list) and all(layer in hashes for layer in layers),'Docker archive references missing config/layer')
            configs.add('sha256:'+hashes[config])
        b.require(configs==set(expected_ids),'Docker archive does not contain exactly the current immutable image configs')
        remote.finish();b.require(reader.bytes==record['bytes'] and reader.sha256==record['sha256'],'Complete image readback SHA256/bytes mismatch')
        return {'full_readback_verified':True,'archive_validated':True,'image_config_ids_verified':sorted(configs),'validation_pipeline_statuses':[{'stage':'gzip_crc_tar_exact_image_configs','returncode':0}],'readback_pipeline_statuses':remote.statuses}
    except BaseException:remote.abort();raise


def configuration_artifact(store,bid,roots,folder):
    # The manifest must live OUTSIDE every scanned root, including capture folder.
    with tempfile.TemporaryDirectory(prefix='weekly-config-manifest-',dir=folder.parent) as temporary:
        manifest=Path(temporary)/'filesystem.jsonl';scope=Path(temporary)/'scope.nul'
        command=[sys.executable,str(Path(b.__file__).with_name('integrity_manifest.py')),'create','--manifest',str(manifest)]
        for root in roots:command+=['--root',root]
        b.quiet_run(command);_,entries=b.read_filesystem_manifest(manifest,roots)
        scope.write_bytes(b''.join(root.lstrip('/').encode()+b'\0' for root in roots));scope.chmod(0o600)
        key='weekly/artifacts/'+bid+'/configuration.tar.zst'
        record=b.pipeline_upload(store,key,b.archive_command(roots,scope),['zstd','--quiet','--stdout','--threads=2','-3']);record['data_key']=key
        proof=b.verify_object(store,record,'precutover_cold',entries)
        receipt=b.verified_record('configuration',key,record,proof);receipt.update(archive_format='tar.zst',roots=roots)
        mkey='weekly/artifacts/'+bid+'/configuration-filesystem-manifest.jsonl';sha,size=b.digest_file(manifest)
        with manifest.open('rb') as stream:head=store.upload(mkey,stream)
        b.require(head['bytes']==size and head.get('encryption')=='AES256','Config filesystem manifest upload differs')
        b.verify_object(store,{'data_key':mkey,'sha256':sha,'bytes':size,'version_id':head.get('version_id')},'configuration')
        receipt.update(filesystem_manifest_key=mkey,filesystem_manifest_sha256=sha,filesystem_manifest_bytes=size,filesystem_manifest_version_id=head.get('version_id'))
        marker='weekly/artifacts/'+bid+'/configuration.verified.json';receipt['manifest_key']=marker;store.put_json(marker,receipt);b.require(store.get_json(marker)==receipt,'Config artifact marker roundtrip failed')
        return receipt


def validate_references(references,bid):
    b.require(isinstance(references,list) and len(references)==2 and {x.get('backup_kind') for x in references}=={'images','configuration'},'Weekly recovery needs both current image/config artifacts')
    for reference in references:
        kind=reference['backup_kind'];ending='images.tar.gz' if kind=='images' else 'configuration.tar.zst'
        b.require(reference.get('schema')==b.SCHEMA and reference.get('bucket')==b.BUCKET and reference.get('success') and reference.get('full_readback_verified') and reference.get('archive_validated'),'Related artifact did not fully verify')
        b.require(reference.get('data_key')=='weekly/artifacts/'+bid+'/'+ending and reference.get('manifest_key')=='weekly/artifacts/'+bid+'/'+kind+'.verified.json','Related weekly artifact points outside its unique recovery set')
        b.require(reference.get('bytes',0)>0 and re.fullmatch('[0-9a-f]{64}',reference.get('sha256','')),'Related artifact digest/size invalid')
        b.require(reference.get('pipeline_statuses') and all(item.get('returncode')==0 for item in reference['pipeline_statuses']),'Related artifact pipeline did not complete')
        if kind=='configuration':
            b.require(reference.get('filesystem_manifest_key')=='weekly/artifacts/'+bid+'/configuration-filesystem-manifest.jsonl','Configuration manifest scope is unsafe')
            b.require(reference.get('filesystem_manifest_bytes',0)>0 and re.fullmatch('[0-9a-f]{64}',reference.get('filesystem_manifest_sha256','')),'Configuration filesystem manifest proof missing')


def owned_point_keys(value,key):
    b.require(b.valid_weekly(value,key),'Weekly point is unverified or malformed')
    keys=[value['data_key'],key]
    references=value.get('related_artifacts')
    if references:
        validate_references(references,value['backup_id'])
        for reference in references:
            keys.extend([reference['data_key'],reference['manifest_key']])
            if reference['backup_kind']=='configuration':keys.append(reference['filesystem_manifest_key'])
    return keys


def prune_sets(store,new_key):
    records=[(key,store.get_json(key)) for key in store.list_keys('weekly/verified/') if key.endswith('.json')]
    expired=b.retention_plan(records,new_key)
    # Validate ALL deletion scopes before deleting even the first expired point.
    deletions=[owned_point_keys(value,key) for key,value in expired]
    latest=store.get_json(new_key);validate_references(latest['related_artifacts'],latest['backup_id'])
    for keys in deletions:
        current=store.get_json(new_key);b.require(current==latest,'Latest complete recovery marker changed during retention')
        for item in [latest,*latest['related_artifacts']]:b.require(store.head(item['data_key'])['ContentLength']==item['bytes'],'Latest linked recovery object unavailable; no old points deleted')
        for item in latest['related_artifacts']:
            b.require(store.get_json(item['manifest_key'])==item,'Latest linked artifact marker changed; no old points deleted')
            if item['backup_kind']=='configuration':b.require(store.head(item['filesystem_manifest_key'])['ContentLength']==item['filesystem_manifest_bytes'],'Latest configuration filesystem manifest unavailable; no old points deleted')
        store.delete(keys)
    return len(deletions)


def database_point(store,bid,references,args):
    validate_references(references,bid)
    key='weekly/data/'+bid+'.sql.gz'
    record=b.pipeline_upload(store,key,['docker','exec',args.container,'sh','-c',b.MYSQL_SHELL],['gzip','-1','-c']);record['data_key']=key
    proof=b.verify_object(store,record,'weekly_mysql');receipt=b.verified_record('weekly_mysql',key,record,proof)
    receipt.update(backup_id=bid,recovery_set_schema='cleanapp-current-weekly-recovery-set-v1',related_artifacts=references,retention_policy={'verified_current_weekly_points':4,'reviewed_s3_noncurrent_expiry_days':2,'current_object_expiration':False,'reviewed_abort_incomplete_multipart_days':1,'permanent_version_deletion_by_helper':False})
    marker='weekly/verified/'+bid+'.json';b.persist_verified(store,receipt,marker,args.receipt)
    deleted=prune_sets(store,marker)
    print(json.dumps({'success':True,'recovery_set_verified':True,'images_config_database_verified':True,'expired_recovery_points_deleted':deleted,'production_config_or_state_changed':False}))
    return receipt


def execute(args,store):
    b.require(sys.platform.startswith('linux') and os.geteuid()==0,'Weekly recovery set requires root on the production AWS Linux host')
    b.require(Path('/etc/cleanapp-runtime/ACTIVE').is_file() and not Path('/etc/cleanapp-runtime/ISOLATED').exists(),'Weekly hot backup requires admitted ACTIVE runtime, never isolated test state')
    b.weekly_preflight(args.container,args.ddl_fenced)
    state=Path(args.state_dir);state.mkdir(parents=True,exist_ok=True,mode=0o700)
    bid=b.identifier()
    pending=state/('weekly-pending-'+bid+'.json')
    b.private_json(pending,{'backup_id':bid,'created_at':b.now(),'bucket':b.BUCKET,'unique_owned_prefix':'weekly/artifacts/'+bid+'/','possible_database_key':'weekly/data/'+bid+'.sql.gz','possible_verified_marker':'weekly/verified/'+bid+'.json','state':'in_progress_or_interrupted','cleanup_rule':'Never delete automatically. Check whether complete point was published before reviewing only these unique owned keys.'})
    with tempfile.TemporaryDirectory(prefix='weekly-current-',dir=state) as temporary:
        folder=Path(temporary);snapshot=capture_current(folder,args.expected_containers);runtime_files=check_runtime_files(snapshot);capture_overlays(folder)
        roots=config_paths(snapshot,args.config_scope,folder,runtime_files)
        runtime.save_private(folder/'recovery-policy.private.json',{'baseline':BASELINE,'ci_pin_files':runtime_files[1:],'current_compose':'compose-current.private.json','restore_isolation_required':True,'never_restore_active_or_job_activation_markers':True,'shared_lock_excluded':LOCK,'read_only_config_bind_mounts_included':True})
        # Snapshot manifest must include recovery-policy before config artifact creation.
        image=image_artifact(store,bid,snapshot);configuration=configuration_artifact(store,bid,roots,folder)
        result=database_point(store,bid,[image,configuration],args);pending.unlink();return result


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--execute',action='store_true');parser.add_argument('--bucket',default=b.BUCKET);parser.add_argument('--region',default='us-east-2');parser.add_argument('--config-scope',default='/etc/cleanapp-backup/weekly-config-scope.json');parser.add_argument('--container',default='cleanapp_db');parser.add_argument('--receipt',default='/var/lib/cleanapp-backup/weekly-last-verified.json');parser.add_argument('--state-dir',default='/var/lib/cleanapp-backup');parser.add_argument('--lock-file',default=LOCK);parser.add_argument('--expected-containers',type=int,default=None);parser.add_argument('--ddl-fenced',action='store_true');args=parser.parse_args()
    if not args.execute:
        print(json.dumps({'execute':False,'sequence':['one outer shared deployment lock','declared production roles/baseline/BOTHpins/reviewedconfig','current images direct gzip stream + full readback','current config direct tar.zst stream + full readback','DB gzip + full readback','publish linked verified point','retain four unique sets, logical deletion only'],'no_cloud_or_production_calls':True}));return
    b.require(args.region=='us-east-2','Only reviewed target region is allowed')
    with b.shared_deployment_lock(args.lock_file):execute(args,b.S3(args.bucket,args.region))

if __name__=='__main__':
    try:main()
    except (b.BackupError,OSError,ValueError,KeyError,EOFError,subprocess.SubprocessError) as error:
        print(json.dumps({'success':False,'error_type':type(error).__name__,'message':str(error) if isinstance(error,b.BackupError) else 'Weekly recovery failed; private diagnostics suppressed'}),file=sys.stderr);sys.exit(1)
