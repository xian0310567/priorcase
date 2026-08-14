#!/bin/sh
# 큐는 비었지만 **불완전하다** — 볼트 하나를 못 읽었다.
# 이걸 "할 일 없음" 으로 그리면 사람은 멀쩡한 줄 안다.
if [ "$1" = "settings" ]; then
cat <<'JSON'
{"config_path":"/tmp/config.toml",
 "vaults":[{"name":"personal","path":"/a","exists":true,"decisions":3,"domains":["proj"]},
           {"name":"work","path":"/b","exists":false,"decisions":0,"domains":[]}],
 "domains":[{"prefix":"proj","folder":"proj","vault":"","paths":[],"repos":[]}],
 "hosts":[{"name":"Claude Code","enabled":true,"root":"/tmp/h","exists":true,"files":1}],
 "warnings":["볼트 work 의 자리가 없다: /b"]}
JSON
exit 0
fi
cat <<'JSON'
{"confirm":[],"review":[],"retro":[],
 "health":[{"name":"볼트 personal","level":"ok","detail":"/a"}],
 "warnings":["볼트 work 에 접근할 수 없다"]}
JSON
