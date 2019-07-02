from cosalib import build


def _builds_return(p):
    return {
        "builds": ["123"],
        "timestamp": "2019-07-12T15:58:57Z"
    }


def test_populate_found_files_with_hash(monkeypatch):
    # Return mocked build.json
    monkeypatch.setattr(build, 'load_json', _builds_return)
    b = build._Build('mocked')

    # We should have no found files
    assert len(b._found_files) == 0
    # Populate found files. Since we have no _produced_files ...
    b._populate_found_files()
    # ... we should still have 0
    assert len(b._found_files) == 0

    # Use this test file as a a _produced_file
    this_test = 'tests/test_build.py'
    b._produced_files.append(this_test)
    # Populate the found_files, request md5 hashes, and expect 1
    b._populate_found_files(hashes=['md5'])
    assert len(b._found_files) == 1
    # Ensure it is the file we expect
    assert b._found_files.get(this_test) is not None
    # And verify we have the minimal set of keys plus md5
    for key in ('local_path', 'path', 'md5', 'size'):
        assert key in b._found_files[this_test].keys()


def test_populate_found_files_no_hash(monkeypatch):
    # Return mocked build.json
    monkeypatch.setattr(build, 'load_json', _builds_return)
    b = build._Build('mocked')

    # We should have no found files
    assert len(b._found_files) == 0
    # Use this test file as a a _produced_file
    this_test = 'tests/test_build.py'
    b._produced_files.append(this_test)
    # Populate the found_files, request md5 hashes, and expect 1
    b._populate_found_files()
    # And verify we have the minimal set of keys that we must have
    for key in ('local_path', 'path', 'size'):
        assert key in b._found_files[this_test].keys()
