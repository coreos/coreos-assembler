#!/usr/bin/env python3
"""
check-new-rpm-urls.py - Check accessibility of new RPM URLs

This script is used in the Tekton task prepare-build-context hosted at
https://gitlab.com/fedora/bootc/tekton-catalog.

When the bump-lockfile job pushes a new commit with updated manifests, two
parallel processes are triggered. The coreos-koji-tagger tags the new RPMs
to the coreos-pool, while Konflux kicks off a pipeline where the task
prefetch-dependencies attempts to pull those RPMs from Koji.

Because Konflux usually triggers faster than the tagger can finish, the
prefetch-dependencies task fails with 404 errors when trying to fetch the
new RPMs that are not yet available.

This script addresses the race condition by extracting the new RPM URLs from
the manifest-lock.*.json diff and checking their accessibility via HTTP HEAD
requests without downloading. If URLs are not yet accessible, it retries
every 5 minutes for up to 30 minutes total. This script runs in the
prepare-build-context task which executes before prefetch-dependencies,
ensuring that all RPMs are available before the download begins.

Usage:
    python check-new-rpm-urls.py [--verbose]

Options:
    --verbose, -v    Display URLs retained after deduplication

Exit codes:
    0    All URLs accessible, or commit is not a lockfile bump
    1    Some URLs still inaccessible after timeout
"""

import argparse
import json
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed  # pylint: disable=no-name-in-module

import requests

EXPECTED_COMMIT_TITLE = "lockfiles: bump to latest"
ARCHES = ["x86_64", "aarch64", "ppc64le", "s390x"]
BASE_URL = "https://kojipkgs.fedoraproject.org/repos-dist/coreos-pool/latest"
MAX_RETRIES = 6
WAIT_MINUTES = 5
WORKERS = 10
TIMEOUT = 30


def main():
    """Main entry point."""
    args = _parse_args()

    if not validate_commit():
        return

    urls, total = extract_new_urls()
    if not urls:
        print("No new packages found in manifest-lock files")
        return

    print(f"Found {len(urls)} new URL(s) to check ({total} total across all arches)")

    if args.verbose:
        print("\nURLs to check:")
        for url in urls:
            print(f"  - {url}")

    print()

    accessible, inaccessible = check_urls_with_retry(urls)
    report_results(accessible, inaccessible)

    if inaccessible:
        sys.exit(1)


def validate_commit() -> bool:
    """Check that the last commit has the expected title."""
    title = _get_last_commit_title()
    print(f'Commit: "{title}"')

    if title != EXPECTED_COMMIT_TITLE:
        print("Not a lockfile bump commit, skipping.")
        return False

    return True


def extract_new_urls() -> tuple[list[str], int]:
    """
    Extract new URLs from all manifests with deduplication.

    Compares manifest-lock.*.json at HEAD vs HEAD~1 to find new or
    updated packages. If a package appears in multiple architectures,
    only the first architecture found (according to ARCHES order) is kept.

    Returns:
        (deduplicated_urls, total_before_dedup)
    """
    seen = set()  # Already seen keys: (name, version-release)
    urls = []
    total = 0

    for arch in ARCHES:
        filename = f"manifest-lock.{arch}.json"
        old_content = _get_file_at_commit("HEAD~1", filename)
        new_content = _get_file_at_commit("HEAD", filename)

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


def _get_last_commit_title() -> str:
    """Get the last commit title via git log."""
    result = subprocess.run(
        ["git", "log", "-1", "--format=%s"],
        capture_output=True,
        text=True,
        check=True,
    )
    return result.stdout.strip()


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
