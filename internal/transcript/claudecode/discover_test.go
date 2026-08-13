package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ★★★ **서브에이전트 기록은 목록에 넣지 않는다.**
//
// 실측(2026-08-13): Claude Code 기록 1,913개 중 **1,417개(74% · 338MB)가
// 서브에이전트**다. 어제 하루에만 278개가 늘었다.
//
// 안 거르면 두 가지가 생긴다.
//
//  1. 체크포인트가 그만큼 쌓인다. 상태 파일은 mutate 마다 통째로 다시 쓰므로
//     그 무게가 **모든 쓰기에** 실린다 (지금 836KB · 3,648항목, 하루 150~280씩 증가).
//  2. 데몬이 도는 중에 생긴 서브에이전트 파일은 0부터 훑힌다 — 한 대화가
//     파일 수만큼 중복 표시될 수 있다.
//
// 잃는 것: 서브에이전트 안에서만 내려지고 부모에게 보고되지 않은 결정. 그건
// 부모가 채택하지 않은 것이므로 프로젝트 결정으로 보기 어렵다.
func TestListSkipsSubagentTranscripts(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-Users-x-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"c851bbeb-e0a9-49bb-aeef-79bccdab0b67.jsonl",
		"9cc2764c-18a1-4ecd-a682-8b5f2f4e78df.jsonl",
	}
	skip := []string{
		"agent-a5801c63bd316cd1b.jsonl",
		"agent-0000000000000000.jsonl",
	}
	for _, n := range append(append([]string{}, want...), skip...) {
		if err := os.WriteFile(filepath.Join(proj, n), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, _, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d개를 줬다 — %d개여야 한다:\n  %s", len(got), len(want), strings.Join(got, "\n  "))
	}
	for _, p := range got {
		if strings.HasPrefix(filepath.Base(p), "agent-") {
			t.Errorf("서브에이전트 기록이 목록에 있다: %s", p)
		}
	}
}

// ★★ **이름으로 판정하는 근거를 못 박는다.**
//
// 내용의 isSidechain 을 보려면 파일을 열어야 하고, 그건 목록을 만들 때마다
// 1,900번 여는 일이다. 이름 판정이 실측에서 **1,844건 전부 내용과 일치**했다
// (불일치 0). 그 규약이 깨지면 이 시험이 아니라 실기기가 먼저 알려 줄 텐데,
// 최소한 판정 함수 자체는 여기서 고정한다.
func TestIsSubagentByName(t *testing.T) {
	for name, want := range map[string]bool{
		"agent-a5801c63bd316cd1b.jsonl":              true,
		"agent-.jsonl":                               true,
		"c851bbeb-e0a9-49bb-aeef-79bccdab0b67.jsonl": false,
		"agentic.jsonl":                              false, // agent- 로 시작하지 않는다
		"my-agent-x.jsonl":                           false, // 중간에 있는 것은 아니다
	} {
		if got := isSubagent(name); got != want {
			t.Errorf("isSubagent(%q) = %v, want %v", name, got, want)
		}
	}
}
