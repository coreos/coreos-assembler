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


def test_image_info(tmpdir):
    cmdlib.run_verbose([
        "qemu-img", "create", "-f", "qcow2", f"{tmpdir}/test.qcow2", "10M"])
    assert cmdlib.image_info(f"{tmpdir}/test.qcow2").get('format') == "qcow2"
    cmdlib.run_verbose([
        "qemu-img", "create", "-f", "vpc",
        '-o', 'force_size,subformat=fixed',
        f"{tmpdir}/test.vpc", "10M"])
    assert cmdlib.image_info(f"{tmpdir}/test.vpc").get('format') == "vpc"


def test_merge_dicts(tmpdir):
    x = {
        1: "Nope",
        2: ["a", "b", "c"],
        3: {"3a": True}
    }

    y = {4: True}

    z = {1: "yup",
         3: {
             "3a": False,
             "3b": "found"
            }
    }

    # merge y into x
    m = cmdlib.merge_dicts(x, y)
    for i in range(1, 4):
        assert i in m
    assert y[4] is True

    # merge z into x
    m = cmdlib.merge_dicts(x, z)
    assert m[1] == "Nope"
    assert x[2] == m[2]
    assert m[3]["3a"] is True
    assert m[3]["3b"] == "found"

   # merge x into z
    m = cmdlib.merge_dicts(z, x)
    assert m[1] == "yup"
    assert x[2] == m[2]
    assert m[3] == z[3]


def test_flatten_image_yaml(tmpdir):
    fn = f"{tmpdir}/image.yaml"
    with open(fn, 'w') as f:
        f.write("""
size: 10
extra-kargs:
  - foobar
unique-key-a: true
""")
    o = cmdlib.flatten_image_yaml(fn)
    assert o['size'] == 10
    assert o['extra-kargs'] == ['foobar']
    assert o['unique-key-a']

    with open(fn, 'a') as f:
        f.write("include: image-base.yaml")
    base_fn = f"{tmpdir}/image-base.yaml"
    with open(base_fn, 'w') as f:
        f.write("""
size: 8
extra-kargs:
  - bazboo
unique-key-b: true
""")
    o = cmdlib.flatten_image_yaml(fn)
    assert o['size'] == 10
    assert o['extra-kargs'] == ['foobar', 'bazboo']
    assert o['unique-key-a']
    assert o['unique-key-b']
