+++
title = "Why does wtg never force-push?"
description = "It never overwrites remote history on its own; it shows you the command instead."
weight = 40
+++

`wtg` is designed to never overwrite remote history automatically. If a branch has diverged (for example after a `wtg rebase`), `wtg push` reports the divergence and shows you the explicit command to run if you wish to overwrite:

{% set id = "sync-and-rebase" %}{% set step = 3 %}{% include "transcript.html" %}
