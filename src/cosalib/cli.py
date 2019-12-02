# NOTE: PYTHONUNBUFFERED is set in the entrypoint for unbuffered output
# pylint: disable=C0103

import argparse
import logging as log
import os


class Cli(argparse.ArgumentParser):
    """
    Abstraction for executing commands from the cli.
    """

    def __init__(self, *args, **kwargs):
        """
        Initializes the Cli instance.

        :param kwargs: All keyword arguments which will pass to ArgumentParser
        :type kwargs: dict
        """
        argparse.ArgumentParser.__init__(self, *args, **kwargs)
        self.add_argument(
            '--log-level', env_var='COSA_LOG_LEVEL', default='info',
            choices=log._nameToLevel.keys(), help='Set the log level')

    def add_argument(self, *args, **kwargs):
        """
        Overloads the add_argument to be able to also read from
        the environment. To read from the environment provide
        the keyword arugment env_var.

        :param args: Non keyword arguments to pass to add_argument
        :type args: list
        :param kwargs: Keyword arguments to pass to add_argument
        :type kwargs: dict
        """
        env_var = kwargs.pop('env_var', None)
        if env_var is not None:
            if kwargs.get('help') is None:
                kwargs['help'] = ''
            kwargs['help'] = kwargs['help'] + ' (Env: {})'.format(env_var)
            default = kwargs.pop('default', None)
            super().add_argument(
                *args, default=os.environ.get(env_var, default), **kwargs)
        else:
            super().add_argument(*args, **kwargs)

    def parse_args(self):
        """
        Parses the arguments passed in, verifies inputs, sets the logger,
        and returns the arguments.

        :returns: The parsed arguments
        :rtype: argparse.Namepsace
        """
        args = super().parse_args()
        self._set_logger(args.log_level)
        return args

    def _set_logger(self, level):
        """
        Set the log level

        :param level: set the log level
        :type level: str
        """
        log.basicConfig(
            format='[%(asctime)s  %(levelname)s]: %(message)s',
            level=log._nameToLevel.get(level.upper(), log.DEBUG))


class BuildCli(Cli):
    """
    Cli class that adds in reusable build specific arguments.
    """

    def __init__(self, *args, **kwargs):
        """
        Initializes the BuildCli instance.

        :param kwargs: All keyword arguments which will pass to ArgumentParser
        :type kwargs: dict
        """
        Cli.__init__(self, *args, **kwargs)
        # Set common arguments
        self.add_argument(
            '--build', default='latest',
            help='Override build id, defaults to latest')
        self.add_argument(
            '--buildroot', default='builds', help='Build diretory')
        self.add_argument(
            '--dump', default=False, action='store_true',
            help='Dump the manfiest and exit')
