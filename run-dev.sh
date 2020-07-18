#!/bin/bash

set -e

go build .
./parent-trap sleep 3600
