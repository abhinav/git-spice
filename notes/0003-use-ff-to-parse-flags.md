# 3. Use ff to parse flags

Date: 2023-05-02

## Status

Accepted

## Context

There are a number of command-line parsing libraries for Go.
The ones I'm familiar with are:

- [Cobra](https://github.com/spf13/cobra):
  This is the most widely used of these libraries,
  so it would normally be the default choice
  except that it's also quite heavyweight.
  It has a very large surface area,
  and it pulls in a small handful of dependencies.
- [jessevdk/go-flags](https://github.com/jessevdk/go-flags):
  Provides a JSON-decoding style API converting flags to structs early.
  This makes it possible to write business logic decoupled from flags.
  However, it's largely unmaintained as of writing this.
  Even beside that, having to put help text inside struct tags
  limited either the readability of the help text or its length.
- [ff](https://github.com/peterbourgon/ff/):
  ff acts as an extension to the standard library's flag package.
  It aims to be lightweight--all its dependencies are optional.

## Decision

In the spirit of a flag *library* rather than a framework,
the project will use ff.
The ffcli subpackage will provide command parsing.

## Consequences

By using ff, we give up the large ecosystem built around Cobra
including completion, man page, and Markdown help generation.
