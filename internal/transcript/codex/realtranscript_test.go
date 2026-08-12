package codex

import (
	"os"
	"testing"

	"github.com/xian0310567/priorcase/internal/transcript"
)

// 실 세션 기록 대조.
//
// **합성 픽스처는 내가 이해한 스키마를 검증할 뿐이다.** 실물이 그 이해와 같은지는
// 따로 확인해야 하고, 호스트가 스키마를 바꾸면 여기서 먼저 깨진다.
//
// CI 에서는 돌지 않는다 (실 기록이 있어야 한다). **읽기만 한다.**
//
//	PRIORCASE_TEST_CODEX=~/.codex/sessions go test ./internal/transcript/codex/ -v
func TestRealCodexSessions(t *testing.T) {
	root := os.Getenv("PRIORCASE_TEST_CODEX")
	if root == "" {
		t.Skip("PRIORCASE_TEST_CODEX 없음 — 실 세션 대조를 건너뛴다")
	}

	paths, unreadable, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("%s 아래에 세션이 없다", root)
	}
	t.Logf("세션 %d개 (못 읽은 디렉토리 %d개)", len(paths), unreadable)

	var files, totalBad int
	var withMeta, withCwd int
	byKind := map[transcript.Kind]int{}

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Errorf("%s 열기 실패: %v", p, err)
			continue
		}
		info, _ := f.Stat()
		turns, meta, consumed, bad, err := Parse(f)
		f.Close()
		if err != nil {
			t.Errorf("%s 파싱 실패: %v", p, err)
			continue
		}
		files++
		totalBad += bad

		// **완결된 파일은 전부 소비돼야 한다.** 마지막 줄이 개행으로 끝나므로
		// consumed 는 파일 크기와 같아야 한다. 어긋나면 체크포인트가 밀린다.
		if info != nil && consumed > info.Size() {
			t.Errorf("%s: consumed(%d) > 파일 크기(%d)", p, consumed, info.Size())
		}
		if meta.SessionID != "" {
			withMeta++
		}
		if meta.Cwd != "" {
			withCwd++
		}
		for _, tn := range turns {
			byKind[tn.Kind]++
		}
	}

	t.Logf("파일 %d개 · 깨진 줄 %d개", files, totalBad)
	t.Logf("발화: user=%d assistant=%d tool=%d thinking=%d",
		byKind[transcript.KindUser], byKind[transcript.KindAssistant],
		byKind[transcript.KindTool], byKind[transcript.KindThinking])
	t.Logf("session_id 있음 %d/%d · cwd 있음 %d/%d", withMeta, files, withCwd, files)

	// **한 종류라도 0 이면 그 축이 통째로 안 잡히는 것이다.**
	if byKind[transcript.KindUser] == 0 {
		t.Error("사람 발화가 하나도 없다 — 스키마가 바뀌었을 수 있다")
	}
	if byKind[transcript.KindAssistant] == 0 {
		t.Error("에이전트 발화가 하나도 없다")
	}
	if byKind[transcript.KindTool] == 0 {
		t.Error("도구 활동이 하나도 없다 — Codex 는 도구를 많이 쓴다")
	}

	// **cwd 는 도메인 해석의 유일한 근거다.** 대부분에서 잡혀야 한다.
	if withCwd*2 < files {
		t.Errorf("cwd 를 %d/%d 에서만 찾았다 — 절반 넘게 기본 도메인으로 떨어진다",
			withCwd, files)
	}

	// 깨진 줄이 많으면 체크포인트가 영영 안 전진한다.
	if totalBad > files {
		t.Errorf("깨진 줄이 %d개다 (파일 %d개) — 스키마 이해가 틀렸을 수 있다", totalBad, files)
	}

	// 사고는 못 읽는 것이 **현재의 사실**이다. 이게 뒤집히면(호스트가 평문을 싣기
	// 시작하면) 알아야 한다 — 그때는 사각이 하나 줄어드는 좋은 소식이다.
	if byKind[transcript.KindThinking] > 0 {
		t.Logf("사고가 %d개 잡혔다 — 암호화가 풀렸다면 문서를 고쳐야 한다",
			byKind[transcript.KindThinking])
	}
}
