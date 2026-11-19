+++
title = "Consuming Read-Only Dependencies"
description = "I only consume a library; keep it pinned and separate from the feature branch."
weight = 60

[extra]
nav_title = "Read-Only Members"
+++

When a workspace only consumes a library or schema repository rather than modifying it, you can add it as a read-only member.

## 1. Adding a Read-Only Member

Add a repository as a detached, read-only worktree:

{% set id = "read-only-member" %}{% set step = 1 %}{% include "transcript.html" %}

Read-only members are included in `go.work` so that the local build resolves against them, but they are not put on the feature branch.

## 2. Checking Status

Inspect the workspace status:

{% set id = "read-only-member" %}{% set step = 2 %}{% include "transcript.html" %}

`wtg status` displays the pinned ref (`◇ read-only @ <ref>`) and will warn if the version required by consuming modules differs from the checked-out commit.
