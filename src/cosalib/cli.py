# NOTE: PYTHONUNBUFFERED is set in the entrypoint for unbuffered output
# pylint: disable=C0103

import argparse
import logging as log
import os

from cosalib import (
    aliyun,
    aws,
    azure,
    digitalocean,
    gcp,
    vultr,
    exoscale,
    ibmcloud,
    kubevirt
)

CLOUD_CLI_TARGET = {
    "aws":          (aws.aws_cli,
                     aws.aws_run_ore,
                     aws.aws_run_ore_replicate),
    "aliyun":       (aliyun.aliyun_cli,
                     aliyun.aliyun_run_ore,
                     aliyun.aliyun_run_ore_replicate),
    "azure":        (azure.azure_cli,
                     azure.azure_run_ore,
                     azure.azure_run_ore_replicate),
    "digitalocean": (digitalocean.digitalocean_cli,
                     digitalocean.digitalocean_run_ore,
                     digitalocean.digitalocean_run_ore_replicate),
    "gcp":          (gcp.gcp_cli,
                     gcp.gcp_run_ore,
                     gcp.gcp_run_ore_replicate),
    "vultr":        (vultr.vultr_cli,
                     vultr.vultr_run_ore,
                     vultr.vultr_run_ore_replicate),
    "exoscale":     (exoscale.exoscale_cli,
                     exoscale.exoscale_run_ore,
                     exoscale.exoscale_run_ore_replicate),
    "ibmcloud":     (ibmcloud.ibmcloud_cli,
                     ibmcloud.ibmcloud_run_ore,
                     ibmcloud.ibmcloud_run_ore_replicate),
    "powervs":      (ibmcloud.ibmcloud_cli,
                     ibmcloud.ibmcloud_run_ore,
                     ibmcloud.ibmcloud_run_ore_replicate),
    "kubevirt":     (kubevirt.kubevirt_cli,
                     kubevirt.kubevirt_run_ore,
                     kubevirt.kubevirt_run_ore_replicate),
}


def cloud_clis():
    return CLOUD_CLI_TARGET.keys()


def get_cloud_ore_cmds(target):
    _, orecmd, orerep = CLOUD_CLI_TARGET[target]
    return orecmd, orerep


def get_cloud_cli(target, parser=None):
    if parser is None:
        parser = BuildCli()
    cli_func, _, _ = CLOUD_CLI_TARGET[target]
    return cli_func(parser)


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
            '--log-level', env_var='COSA_LOG_LEVEL', default='INFO',
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
            if not env_var.startswith('COSA_'):
                env_var = f"COSA_{env_var}"
            ka = kwargs.get("help", '')
            kwargs['help'] = f"{ka} (Env: {env_var})"
            default = kwargs.pop('default', None)
            super().add_argument(
                *args, default=os.environ.get(env_var, default), **kwargs)
        else:
            super().add_argument(*args, **kwargs)

    def parse_args(self, **kwargs):
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
            format='[%(levelname)s]: %(message)s',
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
            '--build', env_var="BUILD", default='latest',
            help='Override build id, defaults to latest')
        self.add_argument(
            '--buildroot', env_var="BUILD_ROOT", default='builds',
            help='Build directory')
        self.add_argument(
            '--schema', env_var="META_SCHEMA",
            default='/usr/lib/coreos-assembler/v1.json',
            help='Schema to use. Set to NONE to skip all validation')
