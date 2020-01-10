import os
import sys
import uuid

import pytest

from cosalib.cli import Cli, BuildCli


def test_cli_add_argument():
    """
    Ensure add_argument works normally
    """
    parser = Cli()
    parser.add_argument('-t', '--test', action='store_true')
    sys.argv = ['', '-t']
    args = parser.parse_args()
    assert args.test is True


def test_cli_add_argument_with_env_var():
    """
    Ensure add_argument works with environment variables
    """
    sys.argv = ['']
    expected = str(uuid.uuid4())
    os.environ['COSA_ENVIRON_TEST'] = expected
    parser = Cli()
    parser.add_argument(
        '-e', '--environ', env_var='ENVIRON_TEST')
    args = parser.parse_args()
    assert args.environ == expected


def test_build_cli_additional_args():
    """
    Ensure that BuildCli contains the expected additional default args
    """
    parser = BuildCli()
    expected = ['--build', '--buildroot', '--dump']
    for action in parser._actions:
        for expect in expected:
            if expect in action.option_strings:
                expected.pop(expected.index(expect))
    if len(expected) != 0:
        pytest.fail(
            'The following actions were missing: {}'.format(
                ', '.join(expected)))
