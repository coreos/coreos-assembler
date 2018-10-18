# Python version of cmdlib.sh

import os,json,tempfile

def write_json(path, data):
    dn = os.path.dirname(path)
    f = tempfile.NamedTemporaryFile(mode='w', dir=dn, delete=False)
    json.dump(data, f)
    os.fchmod(f.file.fileno(), 0o644)
    os.rename(f.name, path)
