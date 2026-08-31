package store

import (
	"reflect"
	"testing"
)

func TestProcedureCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{{
		// 실볼트 `editup-결정-슬랙읽기는-orca브라우저…` 의 절차 절이다.
		// 이 노트가 주입됐는데도 에이전트가 "수단이 없다" 로 끝났다.
		name: "실제 고장 노트",
		body: "## 절차\n\n```bash\n# 1) 채널 ID 추출\n" +
			"orca tab create --url 'https://app.slack.com/client/T017MTC9004'\n" +
			"orca eval --expression '…'\norca goto --url '…'\n```\n",
		want: []string{"orca"},
	}, {
		name: "언어 표시 없는 블록은 안 본다",
		body: "```\n{\"result\": 1}\nsome log line\n```\n",
		want: nil,
	}, {
		// 줄마다 **첫 낱말 하나**만 본다 — 인자를 명령으로 세면 URL·플래그가 섞인다.
		name: "주석과 프롬프트 표시를 벗긴다",
		body: "```sh\n# 설명\n$ orca status\n> orca open\n```\n",
		want: []string{"orca"},
	}, {
		// 셸 문법은 명령 이름이 아니다. 이것만 남으면 절차가 아니다.
		name: "셸 문법만 있으면 안 낸다",
		body: "```bash\ncd /tmp\nexport A=1\nfor x in 1 2; do echo $x; done\n```\n",
		want: nil,
	}, {
		name: "많이 나온 것이 먼저다",
		body: "```bash\naws s3 ls\norca goto\norca eval\norca tab\n```\n",
		want: []string{"orca", "aws"},
	}, {
		name: "블록이 없으면 없다",
		body: "그냥 본문이다. `orca` 를 인라인으로 언급만 한다.",
		want: nil,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := ProcedureCommands([]byte(tc.body))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ProcedureCommands() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 상한을 넘기면 자른다 — 이 줄의 목적은 목록이 아니라 "그 도구가 있다" 이다.
func TestProcedureCommandsCaps(t *testing.T) {
	body := "```bash\na 1\nb 2\nc 3\nd 4\ne 5\n```\n"
	if got := ProcedureCommands([]byte(body)); len(got) != maxProcedureCmds {
		t.Errorf("명령 %d개 — 상한 %d 여야 한다: %v", len(got), maxProcedureCmds, got)
	}
}
