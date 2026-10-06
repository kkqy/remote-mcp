#!/usr/bin/env python3
"""打包四平台产物，并在全部附件上传成功后公开 GitHub Release。"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64")
COMMANDS = ("remote-mcp", "remote-mcp-transfer")
RELEASE_QUERY = "query($owner:String!,$name:String!,$tag:String!){repository(owner:$owner,name:$name){release(tagName:$tag){tagName isDraft}}}"


def release_tag(value):
    if not re.fullmatch(r"v[A-Za-z0-9][A-Za-z0-9._-]{0,62}", value):
        raise argparse.ArgumentTypeError("Invalid release tag; use v followed by letters, digits, dots, underscores or hyphens, at most 64 characters")
    return value


def archive_names(tag):
    release_tag(tag)
    return [f"remote-mcp-{tag}-{target}." + ("tar.gz" if target.startswith("linux-") else "zip") for target in TARGETS]


def file_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def package_release(tag, dist_dir, output_dir):
    names = archive_names(tag)
    output_dir = output_dir.resolve()
    temporary_root = ROOT / ".tmp"
    temporary_root.mkdir(exist_ok=True)
    if not output_dir.is_relative_to(temporary_root.resolve()):
        raise ValueError("Release output directory must be inside the project .tmp directory")
    if output_dir.exists():
        raise ValueError("Release output directory already exists; remove it before packaging")
    inputs = []
    for target in TARGETS:
        files = [dist_dir / target / (command + (".exe" if target.startswith("windows-") else "")) for command in COMMANDS]
        for path in files:
            if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
                raise ValueError(f"Missing or invalid build artifact: {target}/{path.name}")
            if target.startswith("linux-") and not path.stat().st_mode & stat.S_IXUSR:
                raise ValueError(f"Linux build artifact is not executable: {target}/{path.name}")
        inputs.append(files)
    output_dir.parent.mkdir(parents=True, exist_ok=True)
    # 五个文件全部完成后才发布目录，失败不留下可被误用的部分附件。
    with tempfile.TemporaryDirectory(prefix="release-stage-", dir=temporary_root) as directory:
        staging = Path(directory) / "assets"
        staging.mkdir()
        for name, files in zip(names, inputs):
            if name.endswith(".tar.gz"):
                with tarfile.open(staging / name, "w:gz") as archive:
                    for path in files:
                        archive.add(path, arcname=path.name, recursive=False)
            else:
                with zipfile.ZipFile(staging / name, "w", compression=zipfile.ZIP_DEFLATED) as archive:
                    for path in files:
                        archive.write(path, arcname=path.name)
        (staging / "SHA256SUMS").write_text("".join(f"{file_hash(staging / name)}  {name}\n" for name in names), encoding="utf-8")
        staging.rename(output_dir)


def publish_release(tag, asset_dir, repository):
    names = archive_names(tag)
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Invalid repository; expected OWNER/REPO")
    if not asset_dir.is_dir() or {path.name for path in asset_dir.iterdir()} != set(names + ["SHA256SUMS"]):
        raise ValueError("Release assets must contain exactly four platform archives and SHA256SUMS")
    for name in names + ["SHA256SUMS"]:
        path = asset_dir / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"Missing or invalid release asset: {name}")
    expected = "".join(f"{file_hash(asset_dir / name)}  {name}\n" for name in names)
    if (asset_dir / "SHA256SUMS").read_text(encoding="utf-8") != expected:
        raise ValueError("Release asset checksums do not match SHA256SUMS")
    # 单次 GraphQL 查询覆盖草稿和正式版本，任何错误都不能作为不存在。
    owner, name = repository.split("/")
    query = ["gh", "api", "graphql", "-f", "query=" + RELEASE_QUERY, "-f", "owner=" + owner, "-f", "name=" + name, "-f", "tag=" + tag]
    try:
        result = subprocess.run(query, check=True, stdout=subprocess.PIPE, text=True, encoding="utf-8", timeout=60)
    except subprocess.CalledProcessError as error:
        raise ValueError(f"Release lookup failed (gh exit {error.returncode}); check authentication, repository access and connectivity") from error
    except subprocess.TimeoutExpired as error:
        raise ValueError("Release lookup timed out; no release was created") from error
    try:
        response = json.loads(result.stdout)
    except (ValueError, TypeError) as error:
        raise ValueError("Release lookup returned invalid JSON; no release was created") from error
    if not isinstance(response, dict) or ("errors" in response and response["errors"] != []):
        raise ValueError("Release lookup returned GraphQL errors or an invalid response; no release was created")
    data = response.get("data")
    found_repository = data.get("repository") if isinstance(data, dict) else None
    if not isinstance(found_repository, dict) or "release" not in found_repository:
        raise ValueError("Release lookup returned an inaccessible repository or invalid response; no release was created")
    found_release = found_repository["release"]
    if found_release is not None:
        if not isinstance(found_release, dict) or found_release.get("tagName") != tag or type(found_release.get("isDraft")) is not bool:
            raise ValueError("Release lookup returned an invalid release; no release was created")
        raise ValueError("A release or draft with this tag already exists; it will not be overwritten")
    phases = (
        (["gh", "release", "create", tag, "--repo", repository, "--draft", "--verify-tag", "--generate-notes", "--title", tag, *[str((asset_dir / name).resolve()) for name in names + ["SHA256SUMS"]]], "Draft creation or asset upload"),
        (["gh", "release", "edit", tag, "--repo", repository, "--draft=false"], "Release publication"),
    )
    for command, phase in phases:
        try:
            subprocess.run(command, check=True, timeout=300)
        except subprocess.CalledProcessError as error:
            raise ValueError(f"{phase} failed (gh exit {error.returncode}); inspect the release state before retrying; a draft may remain") from error
        except subprocess.TimeoutExpired as error:
            raise ValueError(f"{phase} timed out; inspect the release state before retrying; the final publication result may be unknown") from error


def main():
    parser = argparse.ArgumentParser(description="Package verified Linux/Windows builds or publish their GitHub Release.")
    subparsers = parser.add_subparsers(dest="operation", required=True)
    package = subparsers.add_parser("package", help="Prepare four platform archives and their SHA-256 checksums")
    package.add_argument("--tag", required=True, type=release_tag)
    package.add_argument("--dist-dir", type=Path, default=ROOT / "dist")
    package.add_argument("--output-dir", type=Path, default=ROOT / ".tmp" / "release")
    publish = subparsers.add_parser("publish", help="Upload all assets to a new draft, then publish it")
    publish.add_argument("--tag", required=True, type=release_tag)
    publish.add_argument("--asset-dir", type=Path, default=ROOT / ".tmp" / "release")
    publish.add_argument("--repo", required=True, help="GitHub repository in OWNER/REPO format")
    args = parser.parse_args()
    try:
        if args.operation == "package":
            package_release(args.tag, args.dist_dir, args.output_dir)
            print("Release packages prepared: four archives and SHA256SUMS.")
        else:
            publish_release(args.tag, args.asset_dir, args.repo)
            print("Release published with all five assets.")
    except (ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(f"Release failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
