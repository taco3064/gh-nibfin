#!/bin/sh
# Stand-in for the GitHub runner's runsvc.sh in the host smoke test: it stays
# running until the service manager stops it with SIGTERM.
exec sleep infinity
