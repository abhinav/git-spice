# git-spice configuration will reside in Git configuration

The decision to use `git-config` for git-spice configuration raises the
question whether git-spice configuration will reside in its own file
(that just happens to match Git configuration format)
or whether it will be part of the user's regular Git configuration.

While the former is isolated, it makes for a rougher user experience.
Users either have to edit the file manually or we have to
provide `gs config` commands (which we may do anyway in the future),
as `git config --file=path/to/gs/config` is a bit unwieldy.

On the other hand, if we use regular Git configuration,
besides a familiar path for users to set configuration,
we also get the benefit of Git's configuration hierarchy for free.
Options may be set at system-, user-, repository-, or worktree-level.
The level of flexibility this provides is a good match for more workflows,
and we're able to provide this without adding significant complexity to the UX
to provide similar functionality.
