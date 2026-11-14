#!/usr/bin/env python3
"""Reviewed AWS role identities shared by boot, backup, and isolation helpers.

Keep the migrated 31-role and EPC32 sets valid for rollback. The restored
machine analyzer is admitted only alongside all 32 existing production roles.
Unrelated preview containers are never captured as production roles.
"""
import datetime
import json
import subprocess

LEGACY_SERVICES = frozenset('''
cleanapp_openai_v103 cleanapp_email_fetcher cleanapp_report_listener
cleanapp_report_analyze_human cleanapp_face_detector_consolidated_2
cleanapp_face_detector_consolidated_1 cleanapp_frontend cleanapp_email_service
cleanapp_report_listener_v4 cleanapp_report_renderer_service cleanapp_service
cleanapp_pipelines cleanapp_auth_service cleanapp_frontend_embedded
cleanapp_report_processor cleanapp_report_tags_service cleanapp_db
cleanapp_rabbitmq cleanapp_report_ownership_service cleanapp_voice_assistant_service
cleanapp_customer_service cleanapp_areas_service cleanapp_montenegro_areas
cleanapp_red_bull_dashboard cleanapp_edge_city_areas cleanapp_new_york_areas
cleanapp_devconnect_2025_areas cleanapp_web cleanapp_rabbitmq_exporter
cleanapp_alertmanager cleanapp_prometheus
'''.split())
EPC_SERVICE = 'cleanapp_epc_pusher'
CURRENT_SERVICES = LEGACY_SERVICES | {EPC_SERVICE}
MACHINE_ANALYSIS_SERVICE = 'cleanapp_report_analyze_pipeline'
MACHINE_ANALYSIS_SERVICES = CURRENT_SERVICES | {MACHINE_ANALYSIS_SERVICE}
SUPPORTED_SERVICE_SETS = (LEGACY_SERVICES, CURRENT_SERVICES, MACHINE_ANALYSIS_SERVICES)
PROJECT = 'cleanapp-migrated'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def expected_container(service):
    return 'cleanapp-openai-v103' if service == 'cleanapp_openai_v103' else service


def declared_roles(baseline):
    services = baseline.get('services', {})
    require(baseline.get('name') == PROJECT, 'Unexpected production project identity')
    require(set(services) in SUPPORTED_SERVICE_SETS,
            'Production role set must be the reviewed legacy31, EPC32, or analyzer33 set')
    roles = {service: expected_container(service) for service in services}
    require(all(services[service].get('container_name') == name for service, name in roles.items()),
            'Production container identity differs from its reviewed role')
    return roles


def snapshot_roles(snapshot, expected_names=None, require_running=False):
    containers = snapshot.get('containers', [])
    names = [value.get('Name', '').lstrip('/') for value in containers]
    allowed = [{expected_container(s) for s in services}
               for services in SUPPORTED_SERVICE_SETS]
    require(len(names) == len(set(names)) and set(names) in allowed,
            'Snapshot must contain exactly the reviewed legacy31, EPC32, or analyzer33 identities')
    if expected_names is not None:
        require(set(names) == set(expected_names), 'Captured identities differ from declared production roles')
    if require_running:
        require(all(value.get('State', {}).get('Running') is True for value in containers),
                'Every declared production role must be running during a hot backup')
    return names


def capture_declared_runtime(names, get=None):
    """Capture only named roles; default Docker output stays in private memory."""
    def docker(args):
        result = subprocess.run(['docker', *args], capture_output=True, text=True, timeout=90)
        require(result.returncode == 0, 'Declared runtime inspection failed; raw diagnostics suppressed')
        return json.loads(result.stdout)
    get = get or docker
    containers = get(['inspect', *sorted(names)])
    snapshot = {'schema': 'cleanapp-exact-runtime-v1',
                'captured_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'private_contains_runtime_credentials': True, 'containers': containers}
    snapshot_roles(snapshot, expected_names=names, require_running=True)
    networks = sorted({name for c in containers for name in c['NetworkSettings']['Networks']})
    volumes = sorted({m['Name'] for c in containers for m in c.get('Mounts', []) if m['Type'] == 'volume'})
    images = sorted({c['Image'] for c in containers})
    snapshot['networks'] = get(['network', 'inspect', *networks]) if networks else []
    snapshot['volumes'] = get(['volume', 'inspect', *volumes]) if volumes else []
    snapshot['images'] = get(['image', 'inspect', *images])
    return snapshot
