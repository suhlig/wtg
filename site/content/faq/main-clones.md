+++
title = "Does wtg modify my main clones?"
description = "No. wtg works in linked worktrees and leaves your main clones' branch and HEAD alone."
weight = 10
+++

`wtg` creates linked worktrees alongside your main clones and never changes the working directory, branch, or HEAD of your discovery clones. The one exception is `repo sync`, which fast-forwards the default branch when you explicitly ask it to.
