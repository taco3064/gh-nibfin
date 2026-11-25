#!/bin/sh
# Stand-in for the GitHub runner's config.sh in the host smoke test
# (internal/op/hostsmoke_test.go). It registers nothing: it checks that the
# registration token arrived in ACTIONS_RUNNER_INPUT_TOKEN without storing
# it, prepares .env and .path as the runner's env.sh does, writes the .runner
# settings discovery reads, and counts its runs in .configured.
set -e
[ -n "$ACTIONS_RUNNER_INPUT_TOKEN" ] || { echo "no registration token" >&2; exit 1; }
unset ACTIONS_RUNNER_INPUT_TOKEN
url= name=
while [ $# -gt 0 ]; do
  case $1 in
  --url) url=$2; shift ;;
  --name) name=$2; shift ;;
  --labels) shift ;;
  esac
  shift
done
[ -n "$url" ] && [ -n "$name" ] || { echo "config.sh needs --url and --name" >&2; exit 1; }
[ -e .env ] || touch .env
echo "$PATH" > .path
printf '{"agentId":1,"agentName":"%s","poolId":1,"serverUrl":"https://pipelines.invalid/","gitHubUrl":"%s","workFolder":"_work"}\n' "$name" "$url" > .runner
echo configured >> .configured
