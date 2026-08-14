#!/bin/sh
# 정상 판. 밀린 것도 없고 고장도 없다.
#
# **명령마다 다르게 답한다.** 앱이 queue 와 settings 를 함께 읽으므로, 한 가지만
# 내면 설정 화면이 빈 객체를 받아 통째로 죽는다 — 그건 이 픽스처가 재현하려는
# 상태가 아니다.
if [ "$1" = "settings" ]; then
cat <<'JSON'
{"config_path":"/tmp/config.toml",
 "vaults":[{"name":"default","path":"/tmp/v","exists":true,"decisions":12,"domains":["proj"]}],
 "domains":[{"prefix":"proj","folder":"proj","vault":"","paths":["/tmp/proj"],"repos":[]}],
 "hosts":[{"name":"Claude Code","enabled":true,"root":"/tmp/h/claude","exists":true,"files":656},
          {"name":"Codex CLI","enabled":false,"root":"/tmp/h/codex","exists":true,"files":1729}]}
JSON
exit 0
fi
cat <<'JSON'
{"confirm":[],"review":[],"retro":[],"health":[{"name":"볼트","level":"ok","detail":"/tmp/v"}]}
JSON
