import datetime
import os
import platform
import pytest
import subprocess
import sys
import uuid

sys.path.insert(0, 'src')

from cosalib import cmdlib

PY_MAJOR, PY_MINOR, PY_PATCH = platform.python_version_tuple()


def test_run_verbose():
    """
    Verify run_verbose returns expected information
    """
    result = cmdlib.run_verbose(['echo', 'hi'])
    assert result.stdout is None
    with pytest.raises(FileNotFoundError):
        cmdlib.run_verbose(['idonotexist'])
    # If we are not at least on Python 3.7 we must skip the following test
    if PY_MAJOR == 3 and PY_MINOR >= 7:
        result = cmdlib.run_verbose(['echo', 'hi'], capture_output=True)
        assert result.stdout == b'hi\n'


def test_write_and_load_json(tmpdir):
    """
    Ensure write_json writes loadable json
    """
    data = {
        'test': ['data'],
    }
    path = os.path.join(tmpdir, 'data.json')
    cmdlib.write_json(path, data)
    # Ensure the file exists
    assert os.path.isfile(path)
    # Ensure the data matches
    assert cmdlib.load_json(path) == data


def test_sha256sum_file(tmpdir):
    """
    Verify we get the proper sha256 sum
    """
    test_file = os.path.join(tmpdir, 'testfile')
    with open(test_file, 'w') as f:
        f.write('test')
    # $ sha256sum testfile
    # 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
    e = '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08'
    shasum = cmdlib.sha256sum_file(test_file)
    assert shasum == e


def test_fatal(capsys):
    """
    Ensure that fatal does indeed attempt to exit
    """
    test_string = str(uuid.uuid4())
    err = None
    with pytest.raises(SystemExit) as err:
        cmdlib.fatal(test_string)
    # Check that our test string is in stderr
    assert test_string in str(err)


def test_info(capsys):
    """
    Verify test_info writes properly to stderr without exit
    """
    test_string = str(uuid.uuid4())
    cmdlib.info(test_string)
    captured = capsys.readouterr()
    assert test_string in captured.err


def test_rfc3339_time():
    """
    Verify the format returned from rfc3339_time
    """
    t = cmdlib.rfc3339_time()
    assert datetime.datetime.strptime(t, '%Y-%m-%dT%H:%M:%SZ')
    # now and utcnow don't set TZ's. We should get a raise
    with pytest.raises(AssertionError):
        cmdlib.rfc3339_time(datetime.datetime.now())


def test_rm_allow_noent(tmpdir):
    """
    Ensure rm_allow_noent works both with existing and non existing files
    """
    test_path = os.path.join(tmpdir, 'testfile')
    with open(test_path, 'w') as f:
        f.write('test')
    # Exists
    cmdlib.rm_allow_noent(test_path)
    # Doesn't exist
    cmdlib.rm_allow_noent(test_path)


def test_import_ostree_commit(monkeypatch, tmpdir):
    """
    Verify the correct ostree/tar commands are executed when
    import_ostree_commit is called.
    """
    repo_tmp = os.path.join(tmpdir, 'tmp')
    os.mkdir(repo_tmp)

    class monkeyspcheck_call:
        """
        Verifies each subprocess.check_call matches what is required.
        """
        check_call_count = 0

        def __call__(self, *args, **kwargs):
            if self.check_call_count == 0:
                assert args[0] == [
                    'ostree', 'init', '--repo', tmpdir, '--mode=archive']
            if self.check_call_count == 1:
                assert args[0][0:2] == ['tar', '-C']
                assert args[0][3:5] == ['-xf', 'tarfile']
            if self.check_call_count == 2:
                assert args[0][0:4] == [
                    'ostree', 'pull-local', '--repo', tmpdir]
                assert args[0][5] == 'commit'
            self.check_call_count += 1

    def monkeyspcall(*args, **kwargs):
        """
        Verifies suprocess.call matches what we need.
        """
        assert args[0] == ['ostree', 'show', '--repo', tmpdir, 'commit']

    # Monkey patch the subprocess function
    monkeypatch.setattr(subprocess, 'check_call', monkeyspcheck_call())
    monkeypatch.setattr(subprocess, 'call', monkeyspcall)
    # Test
    cmdlib.import_ostree_commit(tmpdir, 'commit', 'tarfile')


def test_image_info(tmpdir):
    cmdlib.run_verbose([
        "qemu-img", "create", "-f", "qcow2", f"{tmpdir}/test.qcow2", "10M"])
    assert cmdlib.image_info(f"{tmpdir}/test.qcow2").get('format') == "qcow2"
    cmdlib.run_verbose([
        "qemu-img", "create", "-f", "vpc",
        '-o', 'force_size,subformat=fixed',
        f"{tmpdir}/test.vpc", "10M"])
    assert cmdlib.image_info(f"{tmpdir}/test.vpc").get('format') == "vpc"
