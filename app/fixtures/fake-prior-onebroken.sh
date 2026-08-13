#!/bin/sh
# 볼트 하나가 깨졌지만 **나머지 볼트의 큐는 살아 있다.**
# 하나가 깨졌다고 전체를 오류 화면으로 덮으면 할 수 있는 일까지 못 하게 된다.
cat <<'JSON'
{"confirm":[],"review":[],
 "retro":[{"stem":"a-결정-x-2026-08-01","date":"2026-08-01","domain":"a","vault":"personal",
           "summary":"살아 있는 볼트의 결정","reason":"recalled","hits":2}],
 "health":[{"name":"볼트 personal","level":"ok","detail":"/a"},
           {"name":"볼트 work","level":"fail","detail":"접근할 수 없다"}],
 "warnings":["회고 큐를 만들지 못했다: 볼트 work 에 접근할 수 없다"]}
JSON
