package claudecode

import (
	"os"
	"testing"

	"github.com/xian0310567/casebook/internal/transcript"
)

// 실 transcript 대조. 합성 픽스처는 내가 이해한 스키마를 검증할 뿐이라, 실물이
// 그 이해와 같은지는 따로 확인해야 한다. 호스트가 스키마를 바꾸면 여기서 먼저 깨진다.
//
// CI 에서는 돌지 않는다 (실 대화 기록이 있어야 한다). **읽기만 한다.**
func TestRealTranscripts(t *testing.T) {
	root := os.Getenv("CASEBOOK_TEST_TRANSCRIPT")
	if root == "" {
		t.Skip("CASEBOOK_TEST_TRANSCRIPT 없음 — 실 transcript 대조를 건너뛴다")
	}

	paths, unreadable, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("%s 아래에 transcript 가 없다", root)
	}
	t.Logf("transcript %d개 (못 읽은 디렉토리 %d개)", len(paths), unreadable)

	var totalTurns, totalBad, files int
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
			t.Errorf("%s 파싱 에러: %v", p, err)
			continue
		}
		files++
		totalTurns += len(turns)
		totalBad += bad
		for _, tn := range turns {
			byKind[tn.Kind]++
		}

		// 쓰이는 중이 아닌 파일이면 끝까지 소비돼야 한다. 덜 소비됐다는 것은
		// 마지막 줄에 개행이 없다는 뜻이고, 그건 정상(쓰는 중)일 수도 있다.
		if consumed > info.Size() {
			t.Errorf("%s: consumed %d > 파일 크기 %d — 있지도 않은 바이트를 소비했다",
				p, consumed, info.Size())
		}
		if bad > 0 {
			t.Logf("  %s: 깨진 줄 %d개 (전체 %d바이트 중 %d 소비)", p, bad, info.Size(), consumed)
		}
		if len(turns) > 0 && meta.SessionID == "" {
			t.Errorf("%s: 발화가 있는데 SessionID 를 못 뽑았다", p)
		}
	}

	t.Logf("파일 %d개 · 발화 %d개 · 깨진 줄 %d개", files, totalTurns, totalBad)
	for _, k := range []transcript.Kind{transcript.KindUser, transcript.KindAssistant, transcript.KindThinking} {
		t.Logf("  %-10s %d", k, byKind[k])
	}

	// 실 대화라면 사람의 발화가 반드시 있다. 0 이면 스키마를 잘못 읽고 있는 것이다.
	if byKind[transcript.KindUser] == 0 {
		t.Error("사람의 발화가 0개 — 스키마 해석이 틀렸다")
	}
	// 감사 결함 6 의 실측 재확인: 발화 수가 레코드 수보다 훨씬 적어야 한다.
	if byKind[transcript.KindAssistant] == 0 {
		t.Error("에이전트의 발화가 0개 — 스키마 해석이 틀렸다")
	}
}
