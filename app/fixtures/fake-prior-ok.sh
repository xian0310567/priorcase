#!/bin/sh
# queue --json 이 내는 것과 같은 모양. 빈 큐 + 상태 하나.
cat <<'JSON'
{"confirm":[],"review":[],"retro":[],"health":[{"name":"볼트","level":"ok","detail":"/tmp/v"}]}
JSON
