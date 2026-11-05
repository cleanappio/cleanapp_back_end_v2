"""Exercise immutable EC2 rule ownership without any AWS calls."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("aws_ci_ingress", ROOT / "aws_ci_ingress.py")
INGRESS = importlib.util.module_from_spec(spec)
spec.loader.exec_module(INGRESS)
GROUP = "sg-0123456789abcdef0"
OPERATOR = "31.10.148.2/32"
RUNNER = "8.8.8.8/32"
OLD = "sgr-" + "1" * 17
NEW = "sgr-" + "2" * 17
OTHER = "sgr-" + "3" * 17


def rule(identifier, cidr, description="operator"):
    return {"SecurityGroupRuleId": identifier, "GroupId": GROUP, "GroupOwnerId": "654574823950",
            "IsEgress": False, "IpProtocol": "tcp", "FromPort": 22, "ToPort": 22,
            "CidrIpv4": cidr, "Description": description}


class IngressTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.state = Path(self.temporary.name) / "ingress.json"
        self.rules = [rule(OLD, OPERATOR)]
        self.calls = []

    def api(self, operation, group, region, payload=None):
        self.calls.append((operation, group, region, payload))
        self.assertEqual(group, GROUP)
        self.assertEqual(region, "us-east-2")
        if operation == "describe-security-groups":
            return {"SecurityGroups": [{"GroupId": GROUP, "OwnerId": "654574823950"}]}
        if operation == "describe-security-group-rules":
            return {"SecurityGroupRules": copy.deepcopy(self.rules)}
        if operation == "authorize-security-group-ingress":
            self.assertEqual(len(payload), 1)
            permission = payload[0]
            self.assertEqual(permission["IpProtocol"], "tcp")
            self.assertEqual((permission["FromPort"], permission["ToPort"]), (22, 22))
            self.assertEqual(permission["IpRanges"][0]["CidrIp"], RUNNER)
            created = rule(NEW, RUNNER, permission["IpRanges"][0]["Description"])
            self.rules.append(created)
            return {"Return": True, "SecurityGroupRules": [copy.deepcopy(created)]}
        if operation == "revoke-security-group-ingress":
            self.assertEqual(payload, [NEW])
            self.rules = [entry for entry in self.rules if entry["SecurityGroupRuleId"] not in payload]
            return {"Return": True}
        raise AssertionError("Unexpected API operation")

    def open(self, discover=lambda: RUNNER, call=None):
        return INGRESS.open_ingress(GROUP, "us-east-2", self.state, call or self.api, discover, lambda _: None)

    def close(self):
        return INGRESS.close_ingress(GROUP, "us-east-2", self.state, self.api, lambda _: None)

    def test_owned_rule_id_cleanup_preserves_operator_and_other_runner(self):
        self.rules.append(rule(OTHER, "1.1.1.1/32", "other CI"))
        state = self.open()
        self.assertEqual(state["owned_rule_ids"], [NEW])
        self.close()
        self.assertEqual({entry["SecurityGroupRuleId"] for entry in self.rules}, {OLD, OTHER})
        self.assertTrue(json.loads(self.state.read_text())["closed"])

    def test_preexisting_runner_rule_is_never_owned_or_removed(self):
        self.rules.append(rule(OTHER, RUNNER, "foreign CI"))
        self.assertEqual(self.open()["owned_rule_ids"], [])
        self.close()
        self.assertEqual({entry["SecurityGroupRuleId"] for entry in self.rules}, {OLD, OTHER})
        self.assertFalse(any(call[0].startswith(("authorize", "revoke")) for call in self.calls))

    def test_operator_rule_is_never_owned(self):
        self.assertEqual(self.open(discover=lambda: OPERATOR)["owned_rule_ids"], [])
        self.close()
        self.assertEqual([entry["SecurityGroupRuleId"] for entry in self.rules], [OLD])

    def test_uncertain_authorization_recovers_only_unique_run_description(self):
        def timeout(operation, group, region, payload=None):
            if operation == "authorize-security-group-ingress":
                self.assertTrue(self.state.exists())
                self.api(operation, group, region, payload)
                raise RuntimeError("response timeout")
            return self.api(operation, group, region, payload)
        with self.assertRaisesRegex(RuntimeError, "timeout"):
            self.open(call=timeout)
        self.assertTrue(json.loads(self.state.read_text())["pending_authorization"])
        self.close()
        self.assertEqual([entry["SecurityGroupRuleId"] for entry in self.rules], [OLD])

    def test_duplicate_race_does_not_claim_or_delete_foreign_rule(self):
        def duplicate(operation, group, region, payload=None):
            if operation == "authorize-security-group-ingress":
                self.rules.append(rule(OTHER, RUNNER, "another run"))
                raise INGRESS.DuplicateRule("duplicate")
            return self.api(operation, group, region, payload)
        self.assertEqual(self.open(call=duplicate)["owned_rule_ids"], [])
        self.close()
        self.assertIn(OTHER, {entry["SecurityGroupRuleId"] for entry in self.rules})
        self.assertFalse(any(call[0] == "revoke-security-group-ingress" for call in self.calls))

    def test_replaced_foreign_rule_with_same_cidr_is_not_deleted(self):
        self.open()
        self.rules = [entry for entry in self.rules if entry["SecurityGroupRuleId"] != NEW]
        self.rules.append(rule(OTHER, RUNNER, "replacement owned elsewhere"))
        self.close()
        self.assertIn(OTHER, {entry["SecurityGroupRuleId"] for entry in self.rules})
        self.assertFalse(any(call[0] == "revoke-security-group-ingress" for call in self.calls))

    def test_broad_existing_ssh_rule_fails_before_authorization(self):
        self.rules[0]["CidrIpv4"] = "0.0.0.0/0"
        with self.assertRaisesRegex(ValueError, "explicit IPv4"):
            self.open()
        self.assertFalse(self.state.exists())

    def test_all_protocol_rule_fails_before_authorization(self):
        self.rules[0].update(IpProtocol="-1", FromPort=-1, ToPort=-1)
        with self.assertRaisesRegex(ValueError, "all-protocol"):
            self.open()
        self.assertFalse(self.state.exists())

    def test_unspecified_ssh_source_is_rejected(self):
        del self.rules[0]["CidrIpv4"]
        with self.assertRaisesRegex(ValueError, "verifiable IPv4"):
            self.open()
        self.assertFalse(self.state.exists())

    def test_cleanup_removes_owned_id_even_if_other_rule_scope_changes(self):
        self.open()
        self.rules[0]["CidrIpv4"] = "0.0.0.0/0"
        self.close()
        self.assertEqual([entry["SecurityGroupRuleId"] for entry in self.rules], [OLD])

    def test_broad_runner_cidr_is_rejected_before_api_call(self):
        with self.assertRaisesRegex(ValueError, "/32"):
            self.open(discover=lambda: "8.8.8.0/24")
        self.assertEqual(self.calls, [])

    def test_invalid_group_or_other_region_fails_without_api_call(self):
        with self.assertRaisesRegex(ValueError, "reviewed"):
            INGRESS.open_ingress("other", "us-east-2", self.state, self.api, lambda: RUNNER)
        with self.assertRaisesRegex(ValueError, "reviewed"):
            INGRESS.close_ingress(GROUP, "us-west-2", self.state, self.api)
        self.assertEqual(self.calls, [])

    def test_existing_state_must_be_cleaned_before_reopening(self):
        self.open()
        count = len(self.calls)
        with self.assertRaisesRegex(ValueError, "already exists"):
            self.open()
        self.assertEqual(len(self.calls), count)


if __name__ == "__main__":
    unittest.main()
