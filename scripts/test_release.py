"""验证发布产物和 CLI 失败边界，不连接 GitHub 或创建版本。"""
import argparse
from contextlib import redirect_stderr, redirect_stdout
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import release


class PackageTest(unittest.TestCase):
    def setUp(self):
        (release.ROOT / ".tmp").mkdir(exist_ok=True)
        temporary = tempfile.TemporaryDirectory(dir=release.ROOT / ".tmp", prefix="release-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.dist = self.root / "dist"
        self.output = self.root / "output"
        self.tag = "v1.2.3"
        for target in release.TARGETS:
            directory = self.dist / target
            directory.mkdir(parents=True)
            for command in release.COMMANDS:
                name = command + (".exe" if target.startswith("windows-") else "")
                path = directory / name
                path.write_bytes((target + ":" + command).encode("utf-8"))
                path.chmod(0o755)

    @unittest.skipIf(os.name == "nt", "Linux 可执行权限在 Linux 打包 job 与实际压缩包核验")
    def test_packages_have_exact_bytes_permissions_and_hashes(self):
        release.package_release(self.tag, self.dist, self.output)
        manifest = {}
        for line in (self.output / "SHA256SUMS").read_text(encoding="utf-8").splitlines():
            digest, name = line.split("  ")
            manifest[name] = digest
        self.assertEqual(set(manifest), set(release.archive_names(self.tag)))
        self.assertEqual(len(list(self.output.iterdir())), 5)
        for target, name in zip(release.TARGETS, release.archive_names(self.tag)):
            path = self.output / name
            self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), manifest[name])
            if target.startswith("linux-"):
                with tarfile.open(path, "r:gz") as archive:
                    self.assertEqual(archive.getnames(), list(release.COMMANDS))
                    for member in archive.getmembers():
                        self.assertTrue(member.isfile())
                        self.assertEqual(member.mode & 0o777, 0o755)
                        self.assertEqual(archive.extractfile(member).read(), (self.dist / target / member.name).read_bytes())
            else:
                with zipfile.ZipFile(path) as archive:
                    self.assertEqual(archive.namelist(), [command + ".exe" for command in release.COMMANDS])
                    for name in archive.namelist():
                        self.assertEqual(archive.read(name), (self.dist / target / name).read_bytes())

    def test_missing_and_empty_artifacts_fail_without_publishing_directory(self):
        path = self.dist / "linux-amd64" / "remote-mcp"
        for empty in (False, True):
            if empty:
                path.write_bytes(b"")
            else:
                path.unlink()
            with self.subTest(empty=empty), self.assertRaisesRegex(ValueError, "Missing or invalid build artifact"):
                release.package_release(self.tag, self.dist, self.output)
            self.assertFalse(self.output.exists())

    @unittest.skipIf(os.name == "nt", "Windows ACL 无法代替 Unix 可执行权限")
    def test_non_executable_artifact_and_archive_error_fail_closed(self):
        path = self.dist / "linux-amd64" / "remote-mcp"
        path.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "not executable"):
            release.package_release(self.tag, self.dist, self.output)
        path.chmod(0o755)
        with patch.object(release.tarfile, "open", side_effect=OSError("Archive write failed")), self.assertRaises(OSError):
            release.package_release(self.tag, self.dist, self.output)
        self.assertFalse(self.output.exists())

    def test_tag_and_output_boundaries(self):
        for tag in ("v", "1.2.3", "v1/../../file", "v1;echo", "v$(echo)", "v1\n", "v1#label", "v" + "1" * 64):
            with self.subTest(tag=tag), self.assertRaises(argparse.ArgumentTypeError):
                release.package_release(tag, self.dist, self.output)
        with self.assertRaisesRegex(ValueError, "inside the project"):
            release.package_release(self.tag, self.dist, release.ROOT / "release-output")
        self.output.mkdir()
        sentinel = self.output / "existing"
        sentinel.write_bytes(b"preserved")
        with self.assertRaisesRegex(ValueError, "already exists"):
            release.package_release(self.tag, self.dist, self.output)
        self.assertEqual(sentinel.read_bytes(), b"preserved")


class PublishTest(unittest.TestCase):
    def setUp(self):
        (release.ROOT / ".tmp").mkdir(exist_ok=True)
        temporary = tempfile.TemporaryDirectory(dir=release.ROOT / ".tmp", prefix="release-publish-test-")
        self.addCleanup(temporary.cleanup)
        self.assets = Path(temporary.name)
        self.tag = "v1.2.3"
        self.repo = "example/remote-mcp"
        for name in release.archive_names(self.tag):
            (self.assets / name).write_bytes(b"fixed-archive-test-bytes")
        manifest = "".join(f"{hashlib.sha256((self.assets / name).read_bytes()).hexdigest()}  {name}\n" for name in release.archive_names(self.tag))
        (self.assets / "SHA256SUMS").write_text(manifest, encoding="utf-8")
        self.not_found = subprocess.CompletedProcess([], 0, stdout='{"data":{"repository":{"release":null}}}')

    def run_publish(self, effects):
        with patch.object(release.subprocess, "run", side_effect=effects) as command:
            release.publish_release(self.tag, self.assets, self.repo)
        return command.call_args_list

    def test_success_uploads_all_assets_to_draft_before_publication(self):
        calls = self.run_publish([self.not_found, subprocess.CompletedProcess([], 0), subprocess.CompletedProcess([], 0)])
        self.assertEqual(len(calls), 3)
        self.assertEqual(calls[0].args[0], ["gh", "api", "graphql", "-f", "query=" + release.RELEASE_QUERY, "-f", "owner=example", "-f", "name=remote-mcp", "-f", "tag=" + self.tag])
        self.assertEqual(calls[0].kwargs["timeout"], 60)
        self.assertTrue(calls[0].kwargs["check"])
        create, publish = calls[1].args[0], calls[2].args[0]
        self.assertEqual(create[:4], ["gh", "release", "create", self.tag])
        for option in ("--draft", "--verify-tag", "--generate-notes"):
            self.assertIn(option, create)
        self.assertEqual(create[-5:], [str((self.assets / name).resolve()) for name in release.archive_names(self.tag) + ["SHA256SUMS"]])
        self.assertEqual(publish, ["gh", "release", "edit", self.tag, "--repo", self.repo, "--draft=false"])
        for call in calls:
            self.assertIsNot(call.kwargs.get("shell"), True)
            self.assertIn("timeout", call.kwargs)

    def test_numeric_and_boolean_repository_names_remain_strings(self):
        with patch.object(release.subprocess, "run", side_effect=[self.not_found, subprocess.CompletedProcess([], 0), subprocess.CompletedProcess([], 0)]) as command:
            release.publish_release(self.tag, self.assets, "123/true")
        query = command.call_args_list[0].args[0]
        self.assertEqual(query[5:], ["-f", "owner=123", "-f", "name=true", "-f", "tag=" + self.tag])
        self.assertNotIn("-F", query)

    def test_existing_release_and_authentication_error_do_not_publish(self):
        for draft in (False, True):
            found = subprocess.CompletedProcess([], 0, stdout=json.dumps({"data": {"repository": {"release": {"tagName": self.tag, "isDraft": draft}}}}))
            with self.subTest(draft=draft), patch.object(release.subprocess, "run", return_value=found) as command:
                with self.assertRaisesRegex(ValueError, "already exists.*not be overwritten"):
                    release.publish_release(self.tag, self.assets, self.repo)
                self.assertEqual(command.call_count, 1)
                self.assertEqual(command.call_args.args[0][:3], ["gh", "api", "graphql"])
        for diagnostic in ("HTTP 422: already_exists", "HTTP 403: Permission denied"):
            failure = subprocess.CalledProcessError(1, ["gh", "release", "create"], stderr=diagnostic)
            with self.subTest(diagnostic=diagnostic), patch.object(release.subprocess, "run", side_effect=[self.not_found, failure]) as command:
                with self.assertRaisesRegex(ValueError, "Draft creation or asset upload failed.*gh exit 1"):
                    release.publish_release(self.tag, self.assets, self.repo)
                self.assertEqual(command.call_count, 2)
                self.assertEqual(command.call_args.args[0][:4], ["gh", "release", "create", self.tag])
                self.assertTrue(command.call_args.kwargs["check"])
                self.assertNotIn("capture_output", command.call_args.kwargs)

    def test_lookup_errors_and_invalid_responses_never_create(self):
        invalid = ("not-json", "[]", "null", "{}", '{"data":null}', '{"data":{"repository":null}}',
                   '{"data":{"repository":{}}}', '{"data":{"repository":{"release":[]}}}',
                   '{"data":{"repository":{"release":{"tagName":"v-other","isDraft":true}}}}',
                   '{"data":{"repository":{"release":{"tagName":"v1.2.3","isDraft":1}}}}',
                   '{"errors":[{"message":"Access denied"}],"data":{"repository":{"release":null}}}',
                   '{"errors":null,"data":{"repository":{"release":null}}}')
        for response in invalid:
            with self.subTest(response=response), patch.object(release.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, stdout=response)) as command:
                with self.assertRaisesRegex(ValueError, "Release lookup returned"):
                    release.publish_release(self.tag, self.assets, self.repo)
                self.assertEqual(command.call_count, 1)
        for failure in (subprocess.CalledProcessError(1, ["gh", "api", "graphql"]), subprocess.TimeoutExpired(["gh", "api", "graphql"], 60)):
            with self.subTest(failure=type(failure).__name__), patch.object(release.subprocess, "run", side_effect=failure) as command:
                with self.assertRaisesRegex(ValueError, "Release lookup (failed|timed out)"):
                    release.publish_release(self.tag, self.assets, self.repo)
                self.assertEqual(command.call_count, 1)

    def test_upload_failure_does_not_publish_and_publication_failure_is_specific(self):
        for failing_phase in ("create", "edit"):
            effects = [self.not_found]
            if failing_phase == "edit":
                effects.append(subprocess.CompletedProcess([], 0))
            effects.append(subprocess.CalledProcessError(1, ["gh", "release", failing_phase]))
            with self.subTest(failing_phase=failing_phase), patch.object(release.subprocess, "run", side_effect=effects) as command:
                phase = "Draft creation or asset upload" if failing_phase == "create" else "Release publication"
                with self.assertRaisesRegex(ValueError, phase + " failed.*draft may remain"):
                    release.publish_release(self.tag, self.assets, self.repo)
                self.assertEqual(command.call_count, len(effects))
                self.assertEqual([call.args[0][2] for call in command.call_args_list], ["graphql", "create"] if failing_phase == "create" else ["graphql", "create", "edit"])

    def test_upload_timeout_does_not_publish(self):
        effects = [self.not_found, subprocess.TimeoutExpired(["gh", "release", "create"], 300)]
        with patch.object(release.subprocess, "run", side_effect=effects) as command:
            with self.assertRaisesRegex(ValueError, "timed out.*result may be unknown"):
                release.publish_release(self.tag, self.assets, self.repo)
            self.assertEqual(command.call_count, 2)

    def test_missing_modified_or_extra_assets_do_not_contact_github(self):
        name = release.archive_names(self.tag)[0]
        (self.assets / name).write_bytes(b"tampered")
        with patch.object(release.subprocess, "run") as command:
            with self.assertRaisesRegex(ValueError, "checksums"):
                release.publish_release(self.tag, self.assets, self.repo)
            (self.assets / name).unlink()
            with self.assertRaisesRegex(ValueError, "exactly four"):
                release.publish_release(self.tag, self.assets, self.repo)
            (self.assets / "unexpected").write_bytes(b"extra")
            with self.assertRaisesRegex(ValueError, "exactly four"):
                release.publish_release(self.tag, self.assets, self.repo)
            command.assert_not_called()

    def test_cli_failure_has_english_diagnostic_and_no_success(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        argv = ["release.py", "publish", "--tag", self.tag, "--asset-dir", str(self.assets), "--repo", self.repo]
        with patch.object(release.sys, "argv", argv), patch.object(release.subprocess, "run", side_effect=FileNotFoundError("GitHub CLI is not installed")), redirect_stdout(stdout), redirect_stderr(stderr):
            self.assertEqual(release.main(), 1)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("Release failed: GitHub CLI is not installed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
