import copy
import os
import json
import sys
import pytest

parent_path = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, f'{parent_path}/src')
sys.path.insert(0, parent_path)

from cosalib import meta
from cosalib.cmdlib import get_basearch, load_json
from jsonschema import ValidationError


TEST_META_PATH = os.environ.get(
    "COSA_TEST_META_PATH", "/usr/lib/coreos-assembler/fixtures")
TEST_SCHEMA = os.environ.get(
    "COSA_META_SCHEMA", "/usr/lib/coreos-assembler/cosalib/v1.json")


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
    m.read()
    assert m.get('test') == 'changed'
    m.read()
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


def test_merge_meta(tmpdir):
    """
    Verifies merging meta.json works as expected.
    """
    x = None
    y = None

    aws = {
        "path": "/dev/null",
        "size": 99999999,
        "sha256": "ff279bc0207964d96571adfd720b1af1b65e587e589eee528d0315b7fb298773"
    }

    def get_aws(x, key="path"):
        return x.get("images", {}).get("aws", {}).get(key)

    for meta_f in os.listdir(TEST_META_PATH):
        test_meta = os.path.join(TEST_META_PATH, meta_f)
        with open(test_meta, 'r') as valid_data:
            td = json.load(valid_data)
            m = meta.GenericBuildMeta(_create_test_files(tmpdir, meta_data=td),
                                      '1.2.3')

            w = meta.GenericBuildMeta(_create_test_files(tmpdir, meta_data=td),
                                      '1.2.3')
            # create working copies
            if x is None:
                x = copy.deepcopy(m)
            else:
                y = copy.deepcopy(m)

            # add the stamp
            m.write()
            old_stamp = m.get(meta.COSA_VER_STAMP)
            assert old_stamp is not None

            # check merging old into new
            m["images"]["aws"] = aws
            m[meta.COSA_VER_STAMP] = 10

            m.write()
            new_stamp = m.get(meta.COSA_VER_STAMP)
            assert new_stamp > old_stamp
            assert get_aws(m) != aws["path"]

    # Now go full yolo and attempt to merge RHCOS into FCOS
    # Srly? Whose going to do this...
    y._meta_path = x.path
    with pytest.raises(meta.COSAMergeError):
        x.write()

    #### Artifact merging tests
    # clear the meta.json that's been corrupted
    os.unlink(x.path)

    # test that write went to meta.json
    maws = x.write()
    assert x.path == maws

    # test that write went to meta.aws.json
    x.set("coreos-assembler.delayed-meta-merge", True)
    maws = x.write(artifact_name="aws")
    assert maws.endswith("aws.json")

    # make sure that meta.json != meta.aws.json
    x.read()
    d = load_json(maws)
    assert get_aws(m) != get_aws(d)


    # test that the write went to meta.<TS>.json
    tnw = x.write()
    assert maws != tnw

