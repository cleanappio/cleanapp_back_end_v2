#!/usr/bin/env python3
"""Authorize this runner's IPv4 /32, then remove only its owned EC2 rule IDs."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request
import uuid

MANAGED_REGION = "us-east-2"
MANAGED_ACCOUNT = "654574823950"
RULE_ID = re.compile(r"sgr-[0-9a-f]{17}\Z")


class DuplicateRule(RuntimeError):
    pass


def aws(operation, group, region, payload=None):
    arguments = ["aws", "ec2", operation, "--region", region, "--output", "json", "--no-cli-pager"]
    if operation == "describe-security-group-rules":
        arguments += ["--filters", "Name=group-id,Values=" + group]
    elif operation == "describe-security-groups":
        arguments += ["--group-ids", group]
    elif operation == "authorize-security-group-ingress":
        arguments += ["--group-id", group, "--ip-permissions", json.dumps(payload, separators=(",", ":"))]
    elif operation == "revoke-security-group-ingress":
        arguments += ["--group-id", group, "--security-group-rule-ids", *payload]
    else:
        raise ValueError("Unsupported ingress operation")
    result = subprocess.run(arguments, capture_output=True, text=True, check=False)
    if result.returncode:
        if "InvalidPermission.Duplicate" in result.stderr:
            raise DuplicateRule("An equivalent rule already exists")
        raise RuntimeError("EC2 ingress API failed; private CLI diagnostics were not printed")
    return json.loads(result.stdout)


def current_ipv4():
    with urllib.request.urlopen("https://checkip.amazonaws.com", timeout=10) as response:
        address = ipaddress.IPv4Address(response.read(80).decode("ascii").strip())
    if not address.is_global:
        raise ValueError("Runner must have a global public IPv4 address")
    return str(address) + "/32"


def store_state(path, state):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as handle:
        temporary = Path(handle.name)
        os.chmod(temporary, 0o600)
        json.dump(state, handle)
        handle.write("\n")
    temporary.replace(path)


def validate_target(group, region):
    if not re.fullmatch(r"sg-(?:[0-9a-f]{8}|[0-9a-f]{17})", group) or region != MANAGED_REGION:
        raise ValueError("An explicit reviewed EC2 security group in us-east-2 is required")


def rules(response, group):
    result = response["SecurityGroupRules"]
    if not isinstance(result, list) or any(rule.get("GroupId") != group or rule.get("GroupOwnerId") != MANAGED_ACCOUNT for rule in result):
        raise ValueError("Rule response differs from the reviewed group/account")
    return result


def ssh_rules(response, group):
    selected = []
    for rule in rules(response, group):
        if rule.get("IsEgress") is not False:
            continue
        protocol = rule.get("IpProtocol")
        if protocol in {"-1", "all"}:
            raise ValueError("Existing all-protocol ingress must be reviewed privately")
        if protocol not in {"tcp", "6"} or not rule["FromPort"] <= 22 <= rule["ToPort"]:
            continue
        if rule["FromPort"] != 22 or rule["ToPort"] != 22:
            raise ValueError("Existing broad SSH port ranges must be reviewed privately")
        if not rule.get("CidrIpv4") or any(rule.get(key) for key in ["CidrIpv6", "PrefixListId", "ReferencedGroupInfo"]):
            raise ValueError("Existing SSH rules must have verifiable IPv4 CIDR scope")
        if ipaddress.IPv4Network(rule["CidrIpv4"], strict=True).prefixlen != 32:
            raise ValueError("Existing SSH access must contain only explicit IPv4 /32s")
        if not RULE_ID.fullmatch(rule.get("SecurityGroupRuleId", "")):
            raise ValueError("SSH rule has no verifiable immutable rule ID")
        selected.append(rule)
    return selected


def owned_rule_ids(response, state):
    found = []
    for rule in rules(response, state["group_id"]):
        if (rule.get("IsEgress") is False and rule.get("IpProtocol") == "tcp"
                and rule.get("FromPort") == 22 and rule.get("ToPort") == 22
                and rule.get("CidrIpv4") == state["cidr"] and rule.get("Description") == state["description"]):
            identifier = rule.get("SecurityGroupRuleId", "")
            if not RULE_ID.fullmatch(identifier):
                raise ValueError("Owned rule lacks a valid immutable rule ID")
            found.append(identifier)
    return found


def open_ingress(group, region, state_file, call=aws, discover=current_ipv4, pause=time.sleep):
    validate_target(group, region)
    if state_file.exists():
        raise ValueError("Ingress state already exists; clean it up before opening a new lease")
    cidr = discover()
    network = ipaddress.IPv4Network(cidr, strict=True)
    if network.prefixlen != 32 or not network.network_address.is_global:
        raise ValueError("Runner ingress must be one global public IPv4 /32")
    groups = call("describe-security-groups", group, region)["SecurityGroups"]
    if len(groups) != 1 or groups[0].get("GroupId") != group or groups[0].get("OwnerId") != MANAGED_ACCOUNT:
        raise ValueError("Security group differs from the reviewed account")
    before = ssh_rules(call("describe-security-group-rules", group, region), group)
    preexisting = [rule["SecurityGroupRuleId"] for rule in before if rule["CidrIpv4"] == cidr]
    state = {"group_id": group, "region": region, "cidr": cidr,
             "description": "cleanapp-ci:" + uuid.uuid4().hex, "owned_rule_ids": [],
             "pending_authorization": not bool(preexisting), "preexisting_rule_ids": preexisting, "closed": False}
    store_state(state_file, state)
    if preexisting:
        return state
    permission = [{"IpProtocol": "tcp", "FromPort": 22, "ToPort": 22,
                   "IpRanges": [{"CidrIp": cidr, "Description": state["description"]}]}]
    try:
        response = call("authorize-security-group-ingress", group, region, permission)
    except DuplicateRule:
        # Another caller owns the same CIDR permission. Never claim/delete it.
        current = ssh_rules(call("describe-security-group-rules", group, region), group)
        state["preexisting_rule_ids"] = [rule["SecurityGroupRuleId"] for rule in current if rule["CidrIpv4"] == cidr]
        if not state["preexisting_rule_ids"]:
            raise RuntimeError("Duplicate permission could not be verified")
        state["pending_authorization"] = False
        store_state(state_file, state)
        return state
    state["owned_rule_ids"] = owned_rule_ids(response, state)
    if response.get("Return") is not True or len(state["owned_rule_ids"]) != 1:
        raise RuntimeError("EC2 did not return exactly one verifiable owned ingress rule")
    state["pending_authorization"] = False
    store_state(state_file, state)
    old_ids = {rule["SecurityGroupRuleId"] for rule in before}
    for _ in range(15):
        current = ssh_rules(call("describe-security-group-rules", group, region), group)
        ids = {rule["SecurityGroupRuleId"] for rule in current}
        if not old_ids.issubset(ids):
            raise RuntimeError("Existing SSH access changed; review the firewall privately")
        if set(state["owned_rule_ids"]).issubset(ids):
            return state
        pause(2)
    raise RuntimeError("Runner SSH rule did not become available")


def close_ingress(group, region, state_file, call=aws, pause=time.sleep):
    validate_target(group, region)
    if not state_file.exists():
        return
    state = json.loads(state_file.read_text())
    if state.get("group_id") != group or state.get("region") != region:
        raise ValueError("Cleanup state differs from the reviewed target")
    if not re.fullmatch(r"cleanapp-ci:[0-9a-f]{32}", state.get("description", "")):
        raise ValueError("Cleanup lacks a verifiable run ownership marker")
    if ipaddress.IPv4Network(state["cidr"], strict=True).prefixlen != 32:
        raise ValueError("Cleanup refuses any network broader than /32")
    if state.get("closed"):
        return
    if state.get("pending_authorization") and not state["owned_rule_ids"]:
        # A timed-out authorize response can still have created our unique rule.
        for _ in range(5):
            recovered = owned_rule_ids(call("describe-security-group-rules", group, region), state)
            if recovered:
                state["owned_rule_ids"] = recovered
                state["pending_authorization"] = False
                store_state(state_file, state)
                break
            pause(2)
        if not state["owned_rule_ids"]:
            raise RuntimeError("Uncertain authorization could not be resolved; inspect the run ownership marker privately")
    identifiers = state["owned_rule_ids"]
    if any(not RULE_ID.fullmatch(identifier) for identifier in identifiers):
        raise ValueError("Cleanup requires immutable owned security-group-rule IDs")
    for _ in range(5):
        current = rules(call("describe-security-group-rules", group, region), group)
        existing = {rule["SecurityGroupRuleId"] for rule in current}
        remaining = [identifier for identifier in identifiers if identifier in existing]
        if not remaining:
            state["closed"] = True
            store_state(state_file, state)
            return
        try:
            call("revoke-security-group-ingress", group, region, remaining)
        except RuntimeError:
            pass  # A concurrent operator removal is verified by the next read.
        pause(2)
    raise RuntimeError("Owned runner SSH rule cleanup failed; remove only recorded rule IDs privately")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["open", "close"])
    parser.add_argument("--group-id", required=True)
    parser.add_argument("--region", required=True)
    parser.add_argument("--state-file", type=Path, required=True)
    args = parser.parse_args(argv)
    if args.operation == "open":
        open_ingress(args.group_id, args.region, args.state_file)
    else:
        close_ingress(args.group_id, args.region, args.state_file)
    print("Runner-only EC2 SSH ingress " + args.operation + " verified")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("Runner ingress operation failed; inspect its restricted state file privately", file=__import__("sys").stderr)
        raise SystemExit(1)
