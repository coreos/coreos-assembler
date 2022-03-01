Submitting Patches
-------------------

Patches are welcome!  We currently use the vanilla GitHub merge workflow, where
individual pull requests are merged by a human via the web UI.

During the review, any changes should be made as a new commit or an amended
commit.  These can be force pushed to your branch and GitHub **should** be able
to show the "interdiff" between pushes.

Commit Message Style
--------------------

Please look at `git log` and match the commit log style, which is very
similar to the
[Linux kernel](https://git.kernel.org/cgit/linux/kernel/git/torvalds/linux.git).

You may use `Signed-off-by`, but we're not requiring it.

**General Commit Message Guidelines**:

1. Title
    - Specify the context or category of the changes e.g. `lib` for library changes, `docs` for document changes, `bin/<command-name>` for command changes, etc.
    - Begin the title with the first letter of the first word capitalized.
    - Aim for less than 50 characters, otherwise 72 characters max.
    - Do not end the title with a period.
    - Use an [imperative tone](https://en.wikipedia.org/wiki/Imperative_mood).
2. Body
    - Separate the body with a blank line after the title.
    - Begin a paragraph with the first letter of the first word capitalized.
    - Each paragraph should be formatted within 72 characters.
    - Content should be about what was changed and why this change was made.
    - If your commit fixes an issue, the commit message should end with `Closes: #<number>`.

Commit Message example:

```bash
<context>: Less than 50 characters for subject title

A paragraph of the body should be within 72 characters.

This paragraph is also less than 72 characters.
```

For more information see [How to Write a Git Commit Message](https://chris.beams.io/posts/git-commit/)

**Editing a Committed Message:**

To edit the message from the most recent commit run `git commit --amend`. To change older commits on the branch use `git rebase -i`. For a successful rebase, have the branch track `upstream main`. Once the changes have been made and saved, run `git push --force origin <branch-name>`.

Testing Changes
----------------

At a bare minimum, your changes should pass the `tests/check.sh` script.  This is just
some simple syntax checking, but is better than nothing at all.

A more useful test of your changes would include the following:
  - [ ] local build of `coreos-assembler` container image
  - [ ] a build of `coreos-assembler` container image on [Quay.io](https://quay.io)
  - [ ] a successful `coreos-assembler build && coreos-assembler run` of [Fedora CoreOS](https://github.com/coreos/fedora-coreos-config) using your changes in a "rootless" configuration (i.e. no use of `--privileged`)

Code Style
-----------

We currently use a mix of `bash` shell scripts and Python.

The current preference is to use spaces instead of tabs.  We require the use of
spaces when working with Python (as covered in [PEP8](https://www.python.org/dev/peps/pep-0008/#tabs-or-spaces)).
We will tolerate tabs when working with the shell scripts, but we have a strong
preference for spaces.  In both cases, we most commonly use 4 spaces per indent.
Please follow the convention of the file that you are changing and avoid mixing
tabs/spaces whenever possible.

Our enforcement of `bash` style is mostly handled via [ShellCheck](https://github.com/koalaman/shellcheck).

We have a loose of set of [guidelines being discussed](https://github.com/coreos/coreos-assembler/issues/271) about
what we expect for Python style.  The agreed upon items:
  - [ ] 4 spaces per indent; no tabs
  - [ ] target Python 3.6
  - [ ] use `os.path.join()` when constructing paths
  - [ ] use [flake8](https://pypi.org/project/flake8/) to check any changes to existing Python
  - [ ] use [pylint](https://www.pylint.org/) when introducing any new Python files
