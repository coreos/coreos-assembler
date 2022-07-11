from subprocess import check_output, CalledProcessError


def run_cmd(command):
    '''
    Run the given command using check_call/check_output and verify its return code.
    @param command  str command to be executed
    '''
    try:
        output = check_output(command, shell=True)
    except CalledProcessError as e:
        raise SystemExit(f"Failed to invoke command: {e}")
    return output
