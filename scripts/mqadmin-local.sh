#!/usr/bin/env bash
# The remoting SDK has no public delete-subscription-group operation. Use broker tooling.
set -euo pipefail
[[ ${DEXWALLET_TEST_ENV:-} == local ]] || { echo 'Local test environment required' >&2; exit 1; }
[[ ${1:-} =~ ^dexwallet_integration_[a-f0-9]{16}_(consumer|background_background_tasks_v1)$ ]] || { echo 'Exact test consumer group required' >&2; exit 1; }
# The installed broker's official command also removes offsets. No global cleanup.
docker_cmd=(docker)
if ! docker info >/dev/null 2>&1; then docker_cmd=(sudo -n docker); fi
exec "${docker_cmd[@]}" exec rmqbroker sh /home/rocketmq/rocketmq-5.3.2/bin/mqadmin deleteSubGroup -b 127.0.0.1:10911 -g "$1" -r true
