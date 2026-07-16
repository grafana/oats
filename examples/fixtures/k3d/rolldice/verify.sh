#!/bin/sh
set -eu

response=$(curl --fail --silent "$OATS_APP_URL/rolldice")
test "$response" -ge 1
