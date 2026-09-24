#!/usr/bin/env python3
"""
check-new-rpm-urls.py - Check accessibility of new RPM URLs

This script is used in the Tekton task prepare-build-context hosted at
https://gitlab.com/fedora/bootc/tekton-catalog.

When commits modify manifest files (manifest-lock.*.json or
manifest-lock.overrides.yaml), two parallel processes are triggered. The
coreos-koji-tagger tags the new RPMs to the coreos-pool, while Konflux
kicks off a pipeline where the task prefetch-dependencies attempts to pull
those RPMs from Koji.

Because Konflux usually triggers faster than the tagger can finish, the
prefetch-dependencies task fails with 404 errors when trying to fetch the
new RPMs that are not yet available.

This script addresses the race condition by analyzing the last commits
that modified manifest files, extracting new RPM URLs, and checking their
accessibility via HTTP HEAD requests without downloading. If URLs are not
yet accessible, it retries every 5 minutes for up to 30 minutes total.
This script runs in the prepare-build-context task which executes before
prefetch-dependencies, ensuring that all RPMs are available before the
download begins.

Supported manifest files:
    - manifest-lock.*.json (lockfiles with per-architecture packages)
    - manifest-lock.overrides.yaml (fast-track and pinned packages)

Usage:
    python check-new-rpm-urls.py [--verbose]

Options:
    --verbose, -v    Display URLs retained after deduplication

Exit codes:
    0    All URLs accessible, or no manifest changes found
    1    Some URLs still inaccessible after timeout
"""

import argparse
import json
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed  # pylint: disable=no-name-in-module

import requests
import yaml

ARCHES = ["x86_64", "aarch64", "ppc64le", "s390x"]
OVERRIDES_FILE = "manifest-lock.overrides.yaml"
MAX_COMMITS = 5
BASE_URL = "https://kojipkgs.fedoraproject.org/repos-dist/coreos-pool/latest"
MAX_RETRIES = 6
WAIT_MINUTES = 5
WORKERS = 10
TIMEOUT = 30


def main():
    """Main entry point."""
    args = _parse_args()

    commits = _get_manifest_commits()
    if not commits:
        print("No recent commits modifying manifest files")
        return

    print(f"Analyzing {len(commits)} commit(s) that modified manifest files\n")

    all_urls = []
    total = 0
    seen = set()  # For deduplication across commits

    for commit in commits:
        commit_title = _get_commit_title(commit)
        print(f'Commit {commit[:8]}: "{commit_title}"')

        modified_files = _get_files_modified_in_commit(commit)

        # Separate lockfiles and overrides
        lockfiles = [f for f in modified_files
                     if f.startswith("manifest-lock.") and f.endswith(".json")]
        has_overrides = OVERRIDES_FILE in modified_files

        # Process lockfiles
        if lockfiles:
            urls, count = extract_new_urls_from_lockfiles(commit, lockfiles, seen)
            all_urls.extend(urls)
            total += count
            if urls:
                print(f"  - lockfiles: {len(urls)} new URL(s)")

        # Process overrides
        if has_overrides:
            urls, count = extract_new_urls_from_overrides(commit, seen)
            all_urls.extend(urls)
            total += count
            if urls:
                print(f"  - overrides: {len(urls)} new URL(s)")

        if not lockfiles and not has_overrides:
            print("  - no manifest changes")

    if not all_urls:
        print("\nNo new packages found in manifest files")
        return

    print(f"\nFound {len(all_urls)} unique URL(s) to check "
          f"({total} total across all commits/arches)")

    if args.verbose:
        print("\nURLs to check:")
        for url in all_urls:
            print(f"  - {url}")

    print()

    accessible, inaccessible = check_urls_with_retry(all_urls)
    report_results(accessible, inaccessible)

    if inaccessible:
        sys.exit(1)


def extract_new_urls_from_lockfiles(
    commit: str,
    lockfiles: list[str],
    seen: set
) -> tuple[list[str], int]:
    """
    Extract new URLs from lockfiles at a specific commit.

    Compares <commit> vs <commit>~1 for the specified lockfiles.
    If a package appears in multiple architectures, only the first
    architecture found is kept.

    Args:
        commit: The commit hash to analyze
        lockfiles: List of lockfile names to process
        seen: Set of already seen (name, version-release) keys for deduplication

    Returns:
        (urls, total_count)
    """
    urls = []
    total = 0

    for filename in lockfiles:
        # Extract arch from filename: manifest-lock.x86_64.json -> x86_64
        arch = filename.replace("manifest-lock.", "").replace(".json", "")
        if arch not in ARCHES:
            continue

        old_content = _get_file_at_commit(f"{commit}~1", filename)
        new_content = _get_file_at_commit(commit, filename)

        if not new_content:
            continue

        try:
            new_data = json.loads(new_content)
            new_packages = new_data.get("packages", {})
        except json.JSONDecodeError:
            continue

        old_packages = {}
        if old_content:
            try:
                old_data = json.loads(old_content)
                old_packages = old_data.get("packages", {})
            except json.JSONDecodeError:
                pass

        for name, pkg_info in new_packages.items():
            evra = pkg_info.get("evra")
            if not evra:
                continue

            old_pkg_info = old_packages.get(name)
            if old_pkg_info and old_pkg_info.get("evra") == evra:
                continue  # Package unchanged

            total += 1

            # Unique key: name + version-release (without epoch or arch)
            # evra = "1:1.56.1-1.fc44.x86_64" -> vra = "1.56.1-1.fc44.x86_64"
            # vra -> vr = "1.56.1-1.fc44" (without arch)
            vra = evra.split(":", 1)[-1]
            vr = vra.rsplit(".", 1)[0]
            key = (name, vr)

            if key in seen:
                continue  # First arch wins

            seen.add(key)
            url = _build_rpm_url(name, evra, arch)
            urls.append(url)

    return urls, total


def extract_new_urls_from_overrides(commit: str, seen: set) -> tuple[list[str], int]:
    """
    Extract new URLs from manifest-lock.overrides.yaml at a specific commit.

    Compares <commit> vs <commit>~1 to find new or updated packages.

    For packages with 'evr' (no arch): use x86_64 as default arch.
    For packages with 'evra': extract arch from evra.

    Args:
        commit: The commit hash to analyze
        seen: Set of already seen (name, version-release) keys for deduplication

    Returns:
        (urls, total_count)
    """
    urls = []
    total = 0

    old_content = _get_file_at_commit(f"{commit}~1", OVERRIDES_FILE)
    new_content = _get_file_at_commit(commit, OVERRIDES_FILE)

    if not new_content:
        return urls, total

    try:
        new_data = yaml.safe_load(new_content)
        new_packages = new_data.get("packages", {}) if new_data else {}
    except yaml.YAMLError:
        return urls, total

    old_packages = {}
    if old_content:
        try:
            old_data = yaml.safe_load(old_content)
            old_packages = old_data.get("packages", {}) if old_data else {}
        except yaml.YAMLError:
            pass

    for name, pkg_info in new_packages.items():
        if not pkg_info:
            continue

        evr = pkg_info.get("evr")
        evra = pkg_info.get("evra")

        if not evr and not evra:
            continue

        # Check if package changed
        old_pkg_info = old_packages.get(name, {}) or {}
        old_evr = old_pkg_info.get("evr")
        old_evra = old_pkg_info.get("evra")

        if evr and old_evr == evr:
            continue  # Package unchanged
        if evra and old_evra == evra:
            continue  # Package unchanged

        total += 1

        # Build URL using helper
        url = _build_rpm_url_from_overrides(name, evr, evra)
        if not url:
            continue

        # Unique key for deduplication
        if evra:
            # evra: remove epoch and arch suffix
            # e.g., "1:44.6-1.fc44.noarch" -> "44.6-1.fc44"
            vr = evra.split(":", 1)[-1].rsplit(".", 1)[0]
        else:
            # evr: remove epoch only, keep distribution suffix
            # e.g., "1:2026.3-2.fc44" -> "2026.3-2.fc44"
            vr = evr.split(":", 1)[-1]
        key = (name, vr)

        if key in seen:
            continue

        seen.add(key)
        urls.append(url)

    return urls, total


def check_urls_with_retry(urls: list[str]) -> tuple[list[str], list[str]]:
    """Check URLs with retry, returns (accessible, inaccessible)."""
    pending = set(urls)
    accessible = []

    for attempt in range(1, MAX_RETRIES + 1):
        print(f"Checking URLs... (attempt {attempt}/{MAX_RETRIES})")

        results = _check_urls_parallel(list(pending))

        newly_accessible = [url for url, ok in results.items() if ok]
        accessible.extend(newly_accessible)
        pending -= set(newly_accessible)

        print(f"  [{len(accessible)}/{len(urls)}] accessible")

        if not pending:
            print()
            return accessible, []

        if attempt < MAX_RETRIES:
            print(f"  {len(pending)} URL(s) not yet accessible, "
                  f"retrying in {WAIT_MINUTES} minutes...\n")
            time.sleep(WAIT_MINUTES * 60)

    return accessible, list(pending)


def report_results(accessible: list[str], inaccessible: list[str]):
    """Display the final report."""
    total = len(accessible) + len(inaccessible)

    print("=== REPORT ===")

    if not inaccessible:
        print(f"All {total} URL(s) are accessible.")
        return

    print(f"Accessible: {len(accessible)}/{total}\n")
    print(f"WARNING: {len(inaccessible)} URL(s) still inaccessible "
          f"after {MAX_RETRIES * WAIT_MINUTES} minutes:")
    for url in inaccessible:
        print(f"  - {url}")


def _get_file_at_commit(commit: str, filename: str) -> str:
    """Get the content of a file at a specific commit."""
    result = subprocess.run(
        ["git", "show", f"{commit}:{filename}"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        return ""
    return result.stdout


def _get_manifest_commits() -> list[str]:
    """
    Get the last N commits that modified manifest files.

    Returns:
        List of commit hashes (most recent first)
    """
    result = subprocess.run(
        ["git", "log", f"-{MAX_COMMITS}", "--format=%H", "--",
         "manifest-lock.*.json", OVERRIDES_FILE],
        capture_output=True,
        text=True,
        check=True,
    )
    commits = result.stdout.strip().split("\n")
    return [c for c in commits if c]  # Filter empty strings


def _get_files_modified_in_commit(commit: str) -> list[str]:
    """
    Get files modified in a specific commit.

    Returns:
        List of filenames modified in this commit
    """
    result = subprocess.run(
        ["git", "diff-tree", "--no-commit-id", "--name-only", "-r", commit],
        capture_output=True,
        text=True,
        check=True,
    )
    return result.stdout.strip().split("\n")


def _get_commit_title(commit: str) -> str:
    """Get the title of a specific commit."""
    result = subprocess.run(
        ["git", "log", "-1", "--format=%s", commit],
        capture_output=True,
        text=True,
        check=True,
    )
    return result.stdout.strip()


def _build_rpm_url(package_name: str, evra: str, arch: str) -> str:
    """
    Build the RPM URL from name, evra, and arch.

    Args:
        package_name: package name (e.g., "kernel")
        evra: epoch:version-release.arch (e.g., "1:7.0.10-201.fc44.x86_64")
        arch: manifest architecture (e.g., "x86_64")

    Returns:
        Full URL to the RPM on kojipkgs
    """
    # Remove epoch if present (e.g., "1:" at the beginning)
    vra = evra.split(":", 1)[-1]

    # First letter in lowercase
    first_letter = package_name[0].lower()

    # Build the RPM filename
    rpm_filename = f"{package_name}-{vra}.rpm"

    return f"{BASE_URL}/{arch}/Packages/{first_letter}/{rpm_filename}"


def _build_rpm_url_from_overrides(
    name: str,
    evr: str | None,
    evra: str | None
) -> str | None:
    """
    Build RPM URL from overrides file data.

    Args:
        name: package name
        evr: version-release without arch (e.g., "2026.3-2.fc44")
        evra: version-release.arch (e.g., "44.6-1.fc44.noarch")

    Returns:
        Full URL to the RPM, or None if invalid input
    """
    if evra:
        # Extract arch from evra (last component after final dot)
        # e.g., "44.6-1.fc44.noarch" -> arch="noarch", vra="44.6-1.fc44.noarch"
        arch = evra.rsplit(".", 1)[-1]
        # Remove epoch if present (e.g., "1:" at the beginning)
        vra = evra.split(":", 1)[-1]
        # noarch packages are stored in arch-specific directories, use first arch
        if arch == "noarch":
            arch = ARCHES[0]
    elif evr:
        # No arch specified, use x86_64
        arch = "x86_64"
        # Remove epoch if present
        vr = evr.split(":", 1)[-1]
        vra = f"{vr}.{arch}"
    else:
        return None

    first_letter = name[0].lower()
    rpm_filename = f"{name}-{vra}.rpm"

    return f"{BASE_URL}/{arch}/Packages/{first_letter}/{rpm_filename}"


def _check_urls_parallel(urls: list[str]) -> dict[str, bool]:
    """Check multiple URLs in parallel via HTTP HEAD."""
    results = {}

    with ThreadPoolExecutor(max_workers=WORKERS) as executor:
        future_to_url = {
            executor.submit(_check_url_accessible, url): url
            for url in urls
        }

        for future in as_completed(future_to_url):
            url = future_to_url[future]
            try:
                results[url] = future.result()
            except Exception:
                results[url] = False

    return results


def _check_url_accessible(url: str) -> bool:
    """Check if a URL is accessible via HTTP HEAD (status 200)."""
    try:
        response = requests.head(
            url,
            timeout=TIMEOUT,
            allow_redirects=True,
        )
        return response.status_code == 200
    except requests.RequestException:
        return False


def _parse_args() -> argparse.Namespace:
    """Parse command line arguments."""
    parser = argparse.ArgumentParser(
        description="Check accessibility of new RPM URLs"
    )
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Display URLs retained after deduplication"
    )
    return parser.parse_args()


if __name__ == "__main__":
    main()
    sys.exit(0)
