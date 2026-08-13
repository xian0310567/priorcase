#!/bin/sh
# 큐는 비었지만 **불완전하다** — 볼트 하나를 못 읽었다.
# 이걸 "할 일 없음" 으로 그리면 사람은 멀쩡한 줄 안다.
cat <<'JSON'
{"confirm":[],"review":[],"retro":[],
 "health":[{"name":"볼트 personal","level":"ok","detail":"/a"}],
 "warnings":["볼트 work 에 접근할 수 없다"]}
JSON
