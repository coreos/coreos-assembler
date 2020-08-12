import os
import json
import sys
import pytest

parent_path = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, f'{parent_path}/src')
sys.path.insert(0, parent_path)

from cosalib import meta
from cosalib.cmdlib import (
    get_basearch,
    load_json,
    run_verbose,
    write_json
)
from jsonschema import ValidationError

TEST_META_PATH = os.environ.get(
    "COSA_TEST_META_PATH", "/usr/lib/coreos-assembler/fixtures")
TEST_SCHEMA = os.environ.get(
    "COSA_META_SCHEMA", "/usr/lib/coreos-assembler/cosalib/schema/v1.json")


def _create_test_files(tmpdir, meta_data=None):
    """
    Creates test data for each run.
    """
    builds = {
        "schema-version": "1.0.0",
        "builds": [
            {
                "id": "1.2.3",
                "arches": [
                    get_basearch()
                ]
            }
        ],
        "timestamp": "2019-01-1T15:19:45Z"
    }

    if meta_data is None:
        meta_data = {
            'test': 'data',
            'name': 'fedora-coreos',
            'a': {
                'b': 'c',
            }
        }

    buildsdir = os.path.join(tmpdir, 'builds')
    os.makedirs(buildsdir, exist_ok=True)
    with open(os.path.join(buildsdir, 'builds.json'), 'w') as f:
        f.write(json.dumps(builds))
    metadir = os.path.join(
        tmpdir, 'builds', '1.2.3', get_basearch())
    os.makedirs(metadir, exist_ok=True)
    with open(os.path.join(metadir, 'meta.json'), 'w') as f:
        f.write(json.dumps(meta_data))
    return tmpdir


def test_init(tmpdir):
    m = meta.GenericBuildMeta(_create_test_files(tmpdir), '1.2.3', schema=None)
    assert m['test'] is not None


def test_get(tmpdir):
    m = meta.GenericBuildMeta(_create_test_files(tmpdir), '1.2.3', schema=None)
    assert m.get('test') == 'data'
    assert m.get('nope', 'default') == 'default'
    assert m.get(['a', 'b']) == 'c'
    assert m.get(['a', 'd'], 'nope') == 'nope'


def test_set(tmpdir):
    """
    Verify setting works as expected.
    """
    m = meta.GenericBuildMeta(_create_test_files(tmpdir), '1.2.3', schema=None)
    m.set('test', 'changed')
    m.write()
    assert m.get('test') == 'changed'
    m.set(['a', 'b'], 'z')
    m.write()
    assert m.get(['a', 'b']) == 'z'
    assert m['a']['b'] == 'z'
    with pytest.raises(Exception):
        m.set(['i', 'donot', 'exist'], 'boom')


def test_str(tmpdir):
    """
    Verifies the string representation is exactly the same as the
    instance dict.
    """
    m = meta.GenericBuildMeta(_create_test_files(tmpdir), '1.2.3', schema=None)
    assert dict(m) == json.loads(str(m))


def test_valid_schema(tmpdir):
    """
    Verifies that schema testing is enforced and checked against a known-good
    meta.json.
    """
    for meta_f in os.listdir(TEST_META_PATH):
        print(f"Validating {meta_f}")
        test_meta = os.path.join(TEST_META_PATH, meta_f)
        with open(test_meta, 'r') as valid_data:
            td = json.load(valid_data)
            _ = meta.GenericBuildMeta(_create_test_files(tmpdir, meta_data=td),
                                      '1.2.3')


def test_invalid_schema(tmpdir):
    """
    Verifies that schema testing is enforced and checked against a known-good
    meta.json.
    """
    with pytest.raises(ValidationError):
        _ = meta.GenericBuildMeta(_create_test_files(tmpdir), '1.2.3')

def test_require_lock_json(tmpdir):
    """
    Test locking of JSON
    """
    j = {"test": "unwritable"}
    test_f = f"{tmpdir}/test.json"
    test_f_l = f"{tmpdir}/.test.json.lock"
    write_json(test_f, j)
    _ = load_json(test_f, require_exclusive=True)

    with pytest.raises(PermissionError):
        os.chmod(tmpdir, 0o0555)
        _ = load_json(test_f, require_exclusive=True)

    _ = load_json(test_f, require_exclusive=False)