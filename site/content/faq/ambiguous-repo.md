+++
title = "What does the ambiguous repo name error mean?"
description = "Several repos share that short name; give a longer one."
weight = 50
+++

When you pass a short name like `api` or `infra` and multiple discovered repositories share that name (e.g. `example-org/api` and `client/api`), `wtg` refuses to guess. It lists all matching candidates so you can provide the disambiguated path segment or full path:

{% set id = "refusals" %}{% set step = 1 %}{% include "transcript.html" %}
