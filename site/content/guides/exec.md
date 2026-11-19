+++
title = "Running a Command in Every Repo"
description = "Run the tests, or any other command, in every repo of the workspace."
weight = 55

[extra]
nav_title = "Run a Command Everywhere"
+++

`wtg exec` runs one command in each repo's worktree. Put `--` between wtg's own arguments and your command. Inside a workspace the workspace name can be left out.

## 1. One After the Other

By default the repos run in order, and their output streams as it is produced:

{% set id = "exec" %}{% set step = 1 %}{% include "transcript.html" %}

Each repo gets a header line, and ends with `ok` or `failed`.

## 2. All at Once

With `--parallel` the commands run at the same time. The progress line fills in as repos finish; each repo's output is held back and printed in repo order afterwards, so it does not interleave:

{% set id = "exec" %}{% set step = 2 %}{% include "transcript.html" %}

## 3. When a Repo Fails

A failure in one repo does not stop the others. Every repo is attempted, and the failures are listed at the end. The exit code is non-zero, so a script notices. Here `web.go` exists only in `web`, so `test -f web.go` fails in `api`:

{% set id = "exec" %}{% set step = 3 %}{% include "transcript.html" %}

## 4. A Command That Runs Too Long

There is no time limit by default, because exec runs your commands and only you know how long they should take. `--timeout` takes a Go duration such as `90s` or `10m`. A repo that goes over is reported as timed out, and the others carry on:

{% set id = "exec" %}{% set step = 4 %}{% include "transcript.html" %}

### What you have now

The same command has run in every repo of the workspace, and you know which repos it failed in.
