#!/usr/bin/python3

import argparse
import dnf.subject
import hawkey
import os
import yaml
import subprocess

arch = os.uname().machine

# this was partially copied from coreos-koji-tagger


def get_rpminfo(string: str) -> str:
    form = hawkey.FORM_NEVRA

    # get a hawkey.Subject object for the string
    subject = dnf.subject.Subject(string)  # returns hawkey.Subject

    # get a list of hawkey.NEVRA objects that are the possibilities
    nevras = subject.get_nevra_possibilities(forms=form)

    # return the first hawkey.NEVRA item in the list of possibilities
    rpminfo = nevras[0]
    return rpminfo


def is_override_lockfile(filename: str) -> bool:
    return (filename == "manifest-lock.overrides.yaml" or
            filename == f'manifest-lock.overrides.{arch}.yaml')


def assert_epochs_match(overrides_epoch: int, rpmfile_epoch: str):
    # normalize the input into a string
    if overrides_epoch is None:
        normalized_overrides_epoch = '(none)'  # matches rpm -qp --queryformat='%{E}'
    else:
        normalized_overrides_epoch = str(overrides_epoch)
    if normalized_overrides_epoch != rpmfile_epoch:
        raise Exception(f"Epoch mismatch between downloaded rpm ({rpmfile_epoch})"
                        f" and overrides file entry ({overrides_epoch})")


parser = argparse.ArgumentParser(description='Download override RPMs from koji.')
parser.add_argument('--downloaddir', default='overrides/rpm',
                    help='Directory to download override RPMs to (default: overrides/rpm).')
parser.add_argument('--lockfiledir', default='src/config',
                    help='Directory to check lock file (default: src/config).')

args = parser.parse_args()

for path in [args.downloaddir, args.lockfiledir]:
    assert os.path.isdir(path), f"Not found: {path}"

print(f"Download override rpms to {args.downloaddir}/")

rpms = set()
for filename in os.listdir(args.lockfiledir):
    if is_override_lockfile(filename):
        with open(os.path.join(args.lockfiledir, filename)) as f:
            lockfile = yaml.safe_load(f)
        if lockfile is None or 'packages' not in lockfile:
            continue
        for pkg, pkgobj in lockfile['packages'].items():
            if 'evr' in pkgobj:
                rpminfo = get_rpminfo(f"{pkg}-{pkgobj['evr']}.{arch}")
            else:
                rpminfo = get_rpminfo(f"{pkg}-{pkgobj['evra']}")
            rpmnvra = f"{rpminfo.name}-{rpminfo.version}-{rpminfo.release}.{rpminfo.arch}"
            rpms.add(rpmnvra)
            subprocess.check_call(['koji', 'download-build', '--rpm', rpmnvra], cwd=args.downloaddir)
            # Make sure the epoch matches what was in the overrides file
            # otherwise we can get errors: https://github.com/coreos/fedora-coreos-config/pull/293
            cp = subprocess.run(['rpm', '-qp', '--queryformat', '%{E}', f'{rpmnvra}.rpm'],
                check=True,
                capture_output=True,
                cwd=args.downloaddir)
            rpmfile_epoch = cp.stdout.decode('utf-8')
            assert_epochs_match(rpminfo.epoch, rpmfile_epoch)

if not rpms:
    print("No overrides; exiting.")
else:
    for rpm in rpms:
        print(f'Downloaded {rpm} to overrides dir')
