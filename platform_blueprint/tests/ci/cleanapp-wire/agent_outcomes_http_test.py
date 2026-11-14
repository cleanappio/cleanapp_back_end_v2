#!/usr/bin/env python3
"""Bounded disposable HTTP → MySQL → RabbitMQ → stub-analysis outcome fixture.

No external API, real report, paid model, notification or production credential.
Receipt capabilities stay in memory and never appear in diagnostics.
"""
import argparse
import json
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--base-url', default='http://localhost:18082')
parser.add_argument('--compose-file', required=True)
args = parser.parse_args()
origin = urllib.parse.urlsplit(args.base_url)
assert (origin.scheme == 'http' and origin.hostname in {'localhost', '127.0.0.1', '::1'} and
        origin.path in {'', '/'} and not origin.username and not origin.password and
        not origin.query and not origin.fragment), 'Local disposable fixture required'
base = args.base_url.rstrip('/')
endpoint = '/api/v1/public/agent-reports'
compose = str(Path(args.compose_file).resolve())
automatic = {'publication': 'automatic', 'notices': 'automatic'}


def sql(statement):
    result = subprocess.run(['docker', 'compose', '-f', compose, 'exec', '-T', 'mysql', 'mysql',
                             '-uroot', '-proot', 'cleanapp', '-N', '-e', statement],
                            capture_output=True, text=True, timeout=20)
    assert result.returncode == 0, 'Fixture database operation failed; diagnostics suppressed'
    return result.stdout.strip()


def request(method, route, body=None, token=None, timeout=40):
    url = urllib.parse.urljoin(base + '/', route)
    parsed = urllib.parse.urlsplit(url)
    assert parsed.scheme == origin.scheme and parsed.netloc == origin.netloc
    if method == 'POST':
        time.sleep(2.05)  # separate HTTP abuse limiter, not the per-intent quota
    req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), method=method,
                                 headers={'Content-Type': 'application/json'})
    if token:
        req.add_header('Authorization', 'Bearer ' + token)
    try:
        response = urllib.request.urlopen(req, timeout=timeout)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.loads(response.read()), response.headers


def submit(description, handling=None, agent_name=None, with_context=True):
    body = {'description': description, 'source_id': str(uuid.uuid4())}
    if with_context:
        body['url'] = 'https://example.invalid/checkout'
    if agent_name is not None:
        body['agent_name'] = agent_name
    if handling is not None:
        body['handling'] = handling
    code, receipt, headers = request('POST', endpoint, body)
    assert code == 200, ('fixture submit failed', code)
    assert headers.get('Cache-Control') == 'no-store'
    assert receipt['tracking_token'].startswith('gdt_') and receipt['idempotency_replay'] is False
    assert not {'description', 'report_id'} & set(receipt)
    assert receipt['processing_status'] in {'received', 'pending_retry'}, 'Legacy processing vocabulary changed'
    return body, receipt


def final(receipt, assess_routing=False):
    assessed = False
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        code, status, headers = request('GET', receipt['status_url'], token=receipt['tracking_token'],
                                        timeout=min(10, max(0.1, deadline - time.monotonic())))
        assert code == 200 and headers.get('Cache-Control') == 'no-store'
        assert 'tracking_token' not in status and 'description' not in status
        assert status['processing_status'] in {'received', 'pending_retry'}
        outcome = status.get('outcome')
        if (assess_routing and not assessed and outcome and
                outcome['processing']['status'] == 'analyzed' and outcome['publication']['status'] == 'public'):
            assert outcome.get('final') is False, 'Routing not yet assessed cannot be final'
            report_seq = seq(receipt)
            event_state = sql(f"SELECT public_event_status FROM report_outcomes WHERE report_seq={report_seq}")
            if event_state == 'published':
                # Read after observing the committed event marker, rather than
                # relying on a status fetched just before the broker ACK.
                code, after_event, _ = request('GET', receipt['status_url'], token=receipt['tracking_token'],
                                                timeout=min(10, max(0.1, deadline - time.monotonic())))
                assert code == 200
                outcome = after_event['outcome']
                assert outcome.get('final') is False, 'Confirmed internal event is not a producer assessment'
                # Even after the internal broker event completes, absence of
                # an email producer assessment must remain non-final. Inject
                # one schema-valid assessment in this disposable DB; no
                # notification/provider is executed by the fixture.
                channels = {entry['channel']: entry for entry in outcome['delivery']}
                assert channels['email']['status'] == 'pending'
                assert channels['email']['reason_code'] == 'routing_evaluation_pending'
                # Stub brand is empty, which the real EPC producer predicate
                # excludes. No fake EPC send/hold receipt is required.
                assert channels['blockscan']['status'] == 'held'
                assert channels['blockscan']['reason_code'] == 'no_eligible_blockscan_subject'
                sql(f"INSERT INTO email_report_retry(seq,reason,next_attempt_at,retry_count) VALUES({report_seq},'no_valid_emails',DATE_ADD(NOW(),INTERVAL 1 DAY),1)")
                assessed = True
        if outcome and outcome.get('final'):
            assert not assess_routing or assessed, 'Outcome finalized before producer assessment'
            return outcome
        time.sleep(1)
    raise AssertionError('Stub outcome did not finish within the bounded wait')


def seq(receipt):
    identity = receipt['receipt_id']
    assert identity.startswith('rcpt_') and all(c.isalnum() or c in '_-' for c in identity)
    return int(sql("SELECT report_seq FROM wire_submission_receipts WHERE receipt_id='" + identity + "'"))


def retained(receipt, body):
    identity = receipt['receipt_id']
    assert identity.startswith('rcpt_') and all(c.isalnum() or c in '_-' for c in identity)
    report = json.loads(sql("SELECT report_json FROM wire_submissions_raw WHERE receipt_id='" + identity + "'"))
    agent = json.loads(sql("SELECT agent_json FROM wire_submissions_raw WHERE receipt_id='" + identity + "'"))
    assert report['description'] == body['description'], 'Related observation lost its original evidence'
    assert agent.get('agent_name', '') == body.get('agent_name', ''), 'Observation attribution changed'
    return seq(receipt)


# Legacy omission remains a real analysis + private decision, not a permission upgrade.
_, private = submit('I observed the fixture checkout Pay button fail after choosing delivery.')
private_outcome = final(private)
assert private_outcome['processing']['status'] == 'analyzed'
assert private_outcome['decision']['state'] == 'accepted'
assert private_outcome['publication']['status'] == 'private'
assert all(d['status'] == 'not_requested' for d in private_outcome['delivery'])

# A new explicit permission can reach publication without a second review queue.
body, accepted = submit('I observed the fixture checkout Confirm button fail after entering a delivery option.', automatic,
                        agent_name='fixture-primary-agent')
outcome = final(accepted, assess_routing=True)
assert outcome['decision']['state'] == 'accepted'
assert outcome['publication']['status'] == 'public' and outcome['publication'].get('canonical_path', '').startswith('/physical/report/rpt_')
assert not any(d['status'] == 'sent' for d in outcome['delivery']), 'Permission/queueing cannot fabricate delivery'
report_seq = seq(accepted)
assert sql(f'SELECT suppress_rewards FROM report_side_effect_policy WHERE report_seq={report_seq}') == '1'
assert sql(f"SELECT visibility FROM report_raw WHERE report_seq={report_seq}") == 'public'
assert sql(f'SELECT COUNT(*) FROM report_analysis WHERE seq={report_seq} AND language=\'en\'') == '1'
assert sql(f"SELECT source FROM report_analysis WHERE seq={report_seq} AND language='en'") == 'Stub'
analysis_before_replay = sql(f"SELECT SHA2(CONCAT(analysis_text,created_at,updated_at),256) FROM report_analysis WHERE seq={report_seq} AND language='en'")
assert len(analysis_before_replay) == 64

# Retry identity is immutable and unrelated to whether another observation supports the same issue.
code, replay, _ = request('POST', endpoint, body)
assert code == 200 and replay['receipt_id'] == accepted['receipt_id'] and replay['idempotency_replay'] is True
assert request('POST', endpoint, {**body, 'handling': {'publication': 'private', 'notices': 'none'}})[0] == 409
assert sql(f"SELECT SHA2(CONCAT(analysis_text,created_at,updated_at),256) FROM report_analysis WHERE seq={report_seq} AND language='en'") == analysis_before_replay

# Separate observations about the same issue strengthen a cluster candidate;
# they are not request retries and neither original record is discarded.
corroboration_body, corroboration = submit(body['description'], automatic, agent_name='fixture-second-agent')
corroborating_outcome = final(corroboration, assess_routing=True)
assert corroborating_outcome['decision']['state'] == 'cluster_candidate'
assert corroborating_outcome['relationship']['type'] == 'cluster_candidate'
assert any(candidate['reference'] == outcome['publication']['canonical_path'].rsplit('/', 1)[1]
           for candidate in corroborating_outcome['relationship']['candidates'])
assert retained(corroboration, corroboration_body) != retained(accepted, body)
similar_body, similar = submit('After entering a delivery option I observed the fixture checkout Confirm button fail again.', automatic,
                               agent_name='fixture-third-agent')
similar_outcome = final(similar, assess_routing=True)
assert similar_outcome['decision']['state'] == 'cluster_candidate'
assert similar_outcome['publication']['status'] == 'public'
retained(similar, similar_body)

# The same affected product alone does not make a different observation a
# cluster candidate. Every submission keeps its own record and source identity.
_, unrelated = submit('Screen reader navigation becomes trapped inside the account recovery help dialog.', automatic)
unrelated_outcome = final(unrelated, assess_routing=True)
assert unrelated_outcome['decision']['state'] == 'accepted'
assert not unrelated_outcome.get('relationship')

# Full retained text is screened, including beyond the legacy 255-byte projection.
_, sensitive = submit('I observed an error. ' + ('Additional observed context. ' * 15) + 'Contact me at private-person@example.invalid.', automatic)
sensitive_outcome = final(sensitive)
assert sensitive_outcome['decision']['state'] == 'held'
assert sensitive_outcome['publication']['status'] == 'held'
assert sensitive_outcome['next_action']['type'] == 'review_privacy'
assert sensitive_outcome['processing']['status'] == 'not_required'
assert sql(f'SELECT COUNT(*) FROM report_analysis WHERE seq={seq(sensitive)}') == '0'

# Context is checked independently of the classifier's evidence-kind result.
_, missing_context = submit('I observed the button fail after clicking it.', automatic, with_context=False)
missing_outcome = final(missing_context)
assert missing_outcome['decision']['state'] == 'needs_information'
assert missing_outcome['decision']['reason_code'] == 'affected_context_needed'
assert missing_outcome['publication']['status'] == 'private'

_, unknown = submit('[outcome:unknown] The fixture observation does not identify what happened.', automatic)
unknown_outcome = final(unknown)
assert unknown_outcome['decision']['state'] == 'needs_information'
assert unknown_outcome['next_action']['type'] == 'provide_information' and unknown_outcome['next_action'].get('fields')
assert unknown_outcome['publication']['status'] == 'private'

_, coverage = submit('[outcome:coverage] A public source documents inaccessible navigation in the settings panel.', automatic)
coverage_outcome = final(coverage, assess_routing=True)
assert coverage_outcome['decision']['state'] == 'accepted'
assert coverage_outcome['publication']['status'] == 'public'

_, rejected = submit('[outcome:rejected] This fixture text is not a reportable observation.', automatic)
rejected_outcome = final(rejected)
assert rejected_outcome['decision']['state'] == 'rejected' and rejected_outcome['decision'].get('reason_code')
assert rejected_outcome['publication']['status'] == 'private'

# All records remain retained, regardless of their decision.
observations = (private, accepted, corroboration, similar, unrelated, sensitive, missing_context, unknown, coverage, rejected)
assert len({seq(r) for r in observations}) == 10
print(json.dumps({'agent_outcome_http_fixture': 'passed', 'retained_observations': len(observations),
                  'paid_model_calls': 0, 'external_notifications': 0}))
