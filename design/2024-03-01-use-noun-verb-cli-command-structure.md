# Noun-verb CLI command structure

<!--
The filename date preserves this decision's position in the original log.
The exact decision date is unknown.
-->

The CLI will offer commands in the form:

    gs [noun] [verb]

For example:

    gs stack submit
    gs branch create feature1
    gs branch checkout feature1
    gs commit create
    gs commit amend

This structure lends itself well to memorable short-hand aliases for commands.
For example, the above commands could be aliased as:

    gs ss
    gs bc feature1
    gs bco feature1
    gs cc
    gs ca

While it's possible to move some of the subcommands to top-level commands,
it's easier to remember them by a noun defining the scope of the command.
