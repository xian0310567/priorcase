#!/bin/sh
# 볼트 하나가 깨졌지만 **나머지는 살아 있다.**
# 하나가 깨졌다고 전체를 오류 화면으로 덮으면 할 수 있는 일까지 못 하게 된다.
if [ "$1" = "settings" ]; then
cat <<'JSON'
{"config_path":"/tmp/config.toml",
 "vaults":[{"name":"personal","path":"/a","exists":true,"decisions":9,"domains":["a"]},
           {"name":"work","path":"/b","exists":false,"decisions":0,"domains":["b"]}],
 "domains":[{"prefix":"a","folder":"a","vault":"","paths":[],"repos":[]},
            {"prefix":"b","folder":"b","vault":"work","paths":[],"repos":[]}],
 "hosts":[{"name":"Claude Code","enabled":true,"root":"/tmp/h","exists":true,"files":5}],
 "warnings":["볼트 work 의 자리가 없다: /b"]}
JSON
exit 0
fi
cat <<'JSON'
{"confirm":[{"id":"/t.jsonl@1"},{"id":"/t.jsonl@2"}],"review":[],
 "retro":[{"stem":"a-결정-x-2026-08-01"}],
 "health":[{"name":"볼트 personal","level":"ok","detail":"/a"},
           {"name":"볼트 work","level":"fail","detail":"접근할 수 없다"}],
 "warnings":["회고 큐를 만들지 못했다: 볼트 work 에 접근할 수 없다"]}
JSON
