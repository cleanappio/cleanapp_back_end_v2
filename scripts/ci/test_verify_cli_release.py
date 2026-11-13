import copy
import unittest

from verify_cli_release import validate_release


class VerifyCLIReleaseTest(unittest.TestCase):
    def setUp(self):
        self.package = {
            "name": "@cleanapp/cli",
            "version": "0.2.0",
            "private": False,
            "repository": {
                "type": "git",
                "url": "git+https://github.com/cleanappio/cleanapp_back_end_v2.git",
                "directory": "cli/cleanapp",
            },
        }
        self.lock = {"version": "0.2.0", "packages": {"": {"version": "0.2.0"}}}
        self.version_source = 'export const CLI_VERSION = "0.2.0";\n'
        sha = "0123456789abcdef0123456789abcdef01234567"
        self.context = {
            "reviewed_sha": sha,
            "expected_version": "0.2.0",
            "actual_sha": sha,
            "workflow_sha": sha,
            "ref": "refs/heads/main",
            "repository": "cleanappio/cleanapp_back_end_v2",
        }

    def validate(self, *, package=None, lock=None, version_source=None, **context):
        options = {**self.context, **context}
        return validate_release(
            self.package if package is None else package,
            self.lock if lock is None else lock,
            self.version_source if version_source is None else version_source,
            **options,
        )

    def test_reviewed_main_release_is_accepted(self):
        self.assertIsNone(self.validate())

    def test_matching_future_stable_version_is_not_hardcoded_to_current_release(self):
        package = copy.deepcopy(self.package)
        lock = copy.deepcopy(self.lock)
        package["version"] = lock["version"] = lock["packages"][""]["version"] = "1.10.4"
        self.assertIsNone(
            self.validate(
                package=package,
                lock=lock,
                version_source='export const CLI_VERSION = "1.10.4";\n',
                expected_version="1.10.4",
            )
        )

    def test_branch_and_tag_dispatches_cannot_publish(self):
        for ref in ("refs/heads/release", "refs/tags/v0.2.0", "refs/heads/main/other", "main"):
            with self.subTest(ref=ref), self.assertRaises(ValueError):
                self.validate(ref=ref)

    def test_fork_or_case_changed_repository_cannot_publish(self):
        for repository in (
            "someone/cleanapp_back_end_v2",
            "cleanappio/another-repository",
            "CleanAppIO/cleanapp_back_end_v2",
        ):
            with self.subTest(repository=repository), self.assertRaises(ValueError):
                self.validate(repository=repository)

    def test_actual_and_workflow_source_must_both_equal_reviewed_commit(self):
        other = "fedcba9876543210fedcba9876543210fedcba98"
        for key in ("reviewed_sha", "actual_sha", "workflow_sha"):
            with self.subTest(mismatched=key), self.assertRaises(ValueError):
                self.validate(**{key: other})

    def test_reviewed_commit_requires_full_lowercase_hex_not_a_ref(self):
        for reviewed in (
            "main",
            self.context["reviewed_sha"][:12],
            self.context["reviewed_sha"].upper(),
            "g" * 40,
            " " + self.context["reviewed_sha"],
            self.context["reviewed_sha"] + "\n",
        ):
            with self.subTest(reviewed=reviewed), self.assertRaises(ValueError):
                # Make all three agree so this proves format validation rather
                # than merely detecting a mismatch between source identities.
                self.validate(reviewed_sha=reviewed, actual_sha=reviewed, workflow_sha=reviewed)

    def test_expected_version_must_be_stable_even_when_all_files_agree(self):
        for version in ("0.2", "v0.2.0", "0.2.0-rc.1", "0.2.0+build.1", "0.two.0", "0.2.0\n"):
            package = copy.deepcopy(self.package)
            lock = copy.deepcopy(self.lock)
            package["version"] = lock["version"] = lock["packages"][""]["version"] = version
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.validate(
                    package=package,
                    lock=lock,
                    version_source=f'export const CLI_VERSION = "{version}";\n',
                    expected_version=version,
                )

    def test_every_shipped_version_must_equal_the_reviewed_version(self):
        for field in ("package", "lock", "lock-root", "source"):
            package = copy.deepcopy(self.package)
            lock = copy.deepcopy(self.lock)
            source = self.version_source
            if field == "package":
                package["version"] = "0.2.1"
            elif field == "lock":
                lock["version"] = "0.2.1"
            elif field == "lock-root":
                lock["packages"][""]["version"] = "0.2.1"
            else:
                source = 'export const CLI_VERSION = "0.2.1";\n'
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.validate(package=package, lock=lock, version_source=source)

    def test_incomplete_lock_or_missing_export_cannot_bypass_version_guard(self):
        for lock in ({"packages": {"": {"version": "0.2.0"}}}, {"version": "0.2.0"}, {"version": "0.2.0", "packages": {}}):
            with self.subTest(lock=lock), self.assertRaises(ValueError):
                self.validate(lock=lock)
        with self.assertRaises(ValueError):
            self.validate(version_source="// CLI version is not exported here\n")

    def test_package_identity_and_explicit_public_intent_are_required(self):
        for name in ("@someone/cli", "cleanapp", "@cleanapp/other", "@CleanApp/cli"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                self.validate(package={**self.package, "name": name})
        for private in (True, None, 0, "false"):
            with self.subTest(private=private), self.assertRaises(ValueError):
                self.validate(package={**self.package, "private": private})
        package = copy.deepcopy(self.package)
        del package["private"]
        with self.assertRaises(ValueError):
            self.validate(package=package)

    def test_provenance_repository_metadata_must_match_the_publishing_repository(self):
        bad_metadata = (
            None,
            "https://github.com/cleanappio/cleanapp_back_end_v2",
            {**self.package["repository"], "type": "svn"},
            {**self.package["repository"], "url": "git+https://github.com/someone/cleanapp_back_end_v2.git"},
            {**self.package["repository"], "directory": "another-package"},
            {**self.package["repository"], "unexpected": "metadata"},
        )
        for metadata in bad_metadata:
            with self.subTest(metadata=metadata), self.assertRaises(ValueError):
                self.validate(package={**self.package, "repository": metadata})
        package = copy.deepcopy(self.package)
        del package["repository"]
        with self.assertRaises(ValueError):
            self.validate(package=package)


if __name__ == "__main__":
    unittest.main()
