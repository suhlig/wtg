+++
title = "Does wtg work for non-Go repositories?"
description = "Yes. It only writes a go.work file when a repo has a go.mod."
weight = 80
+++

Yes! `wtg` works with repositories in any programming language. It only generates a `go.work` file when at least one repository in the workspace contains a `go.mod` file.
