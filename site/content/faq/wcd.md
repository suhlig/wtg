+++
title = "Why must wcd be a shell function?"
description = "A child process cannot change its parent shell's directory."
weight = 90
+++

A child process (such as a CLI binary) cannot change the working directory of its parent shell. Therefore, `wcd` must be a shell function that calls `cd "$(wtg path "$1")"`.
