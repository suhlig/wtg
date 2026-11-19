+++
title = "What is the sibling block in my pull request description?"
description = "A Markdown block that links the pull requests of one workspace to each other."
weight = 60
+++

When you land a workspace using `wtg land` with the `pr` strategy, `wtg` opens one pull request per repo:

{% set id = "land-with-pr" %}{% set step = 1 %}{% include "transcript.html" %}

and then updates each description with a markdown block that links its siblings. Here is the description of the `api` pull request afterwards:

<p class="note"><strong>Note:</strong> <code>gh</code> is simulated here; git is real.</p>

{% set id = "land-with-pr" %}{% set step = 3 %}{% include "transcript.html" %}

Pass `--no-link` to `wtg land` if you want to suppress this sibling block.
