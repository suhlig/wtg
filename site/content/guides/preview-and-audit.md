+++
title = "Previewing and Auditing Operations"
description = "Preview and audit what wtg does with --dry-run and --summary."
weight = 70
+++

`wtg` provides two global flags that work with any mutating command to help you understand and audit its actions.

## 1. Dry Run Preview (`--dry-run`)

Run any command with `--dry-run` to preview the exact plan without executing mutating operations:

{% set id = "preview-and-audit" %}{% set step = 1 %}{% include "transcript.html" %}

## 2. Execution Summary (`--summary`)

Run a command with `--summary` to execute normally and print a complete log of all git commands, filesystem mutations, and helper calls:

{% set id = "preview-and-audit" %}{% set step = 2 %}{% include "transcript.html" %}
