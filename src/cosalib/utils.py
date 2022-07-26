import logging
import subprocess

# Set local logging
logger = logging.getLogger(__name__)
logger.setLevel(logging.DEBUG)


def runcmd(cmd: list, **kwargs: int) -> subprocess.CompletedProcess:
    '''
    Run the given command using subprocess.run and perform verification.
    @param cmd: list that represents the command to be executed
    @param kwargs: key value pairs that represent options to run()
    '''
    try:
        # default args to pass to subprocess.run
        pargs = {"check": True, "capture_output": True}
        logger.debug(f"Running command: {cmd}")
        pargs.update(kwargs)
        cp = subprocess.run(cmd, **pargs)
    except subprocess.CalledProcessError as e:
        logger.error("Command returned bad exitcode")
        logger.error(f"COMMAND: {cmd}")
        logger.error(f" STDOUT: {e.stdout.decode()}")
        logger.error(f" STDERR: {e.stderr.decode()}")
        raise e
    return cp  # subprocess.CompletedProcess
