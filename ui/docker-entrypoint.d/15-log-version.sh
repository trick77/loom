#!/bin/sh
# The nginx image runs every /docker-entrypoint.d/*.sh at container start. Log the
# baked-in build version so the running UI image is identifiable from the logs,
# mirroring the backend's "starting lume version=..." line.
echo "starting lume-ui version=${UI_VERSION:-dev}"
