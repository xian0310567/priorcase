package hook

import (
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/core/store"
)

// minPromptLen 은 이보다 짧은 프롬프트에서는 회수하지 않는다.
//
// "고마워"·"응"·"ㅇㅇ" 같은 프롬프트에도 훅이 발동해서, 옛 구현의 로그가
// `stage=no-match` 와 억지 매칭으로 가득했다. 짧은 프롬프트는 주제를 담지 않는다.
const minPromptLen = 8

// userPromptSubmit 은 **이 어댑터의 존재 이유**다.
//
// MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 주제가 바뀌는 순간
// 과거 결정을 강제로 들이미는 것은 호스트 훅으로만 된다 (스펙 §9 의 굵은 칸).
func (o Options) userPromptSubmit() error {
	prompt := strings.TrimSpace(o.Input.Prompt)
	if len([]rune(prompt)) < minPromptLen {
		return nil
	}

	// **제외 구역에서도 회수한다.** 쓰기 경로만 막는 것이지 읽기까지 막을 이유가 없다.
	// NOI 처럼 자체 스키마를 쓰는 저장소에서도 common 교훈은 꺼내 써야 하고, 회수는
	// 읽기 전용이라 그 저장소의 규약을 건드릴 수 없다.
	hits, skipped, err := search.Recall(o.Layout, o.Config, prompt, search.Options{
		Cwd: o.Input.Cwd, CrossProject: true, Limit: 3, MinScore: 1,
	})
	if err != nil {
		return err
	}

	// 건너뛴 노트는 **stderr 로만** 낸다. 여기 stdout 은 훅이 그대로 컨텍스트에
	// 밀어넣는 순수 데이터라 한 줄이라도 섞이면 "[과거 결정 참조]" 블록이 오염된다.
	warnSkipped(o.Err, o.Layout, skipped)

	// 렌더러를 새로 만들지 않는다 — 이 프로젝트가 "방출기 두 벌" 을 죄목으로 든다.
	if s := search.RenderInject(o.Layout, hits); s != "" {
		fmt.Fprint(o.Out, s)
	}
	return nil
}

// warnSkipped 는 읽지 못한 노트를 stderr 로 알린다. cli 의 같은 이름 함수와 문구가
// 비슷하지만 **어댑터끼리 공유하지 않는다** (§4.1) — 공유하려면 core 로 내려야 하는데,
// 이건 표시 문제라 core 의 관심사가 아니다.
func warnSkipped(w interface{ Write([]byte) (int, error) }, l *store.Layout, skipped []store.SkippedNote) {
	if len(skipped) == 0 || w == nil {
		return
	}
	fmt.Fprintf(w, "경고: 결정 노트 %d건을 읽지 못해 회수에서 빠졌다:\n", len(skipped))
	for _, s := range skipped {
		reason := strings.ReplaceAll(strings.TrimRight(fmt.Sprint(s.Reason), "\n"), "\n", "\n      ")
		fmt.Fprintf(w, "  - %s\n      %s\n", l.RelPath(s.Path), reason)
	}
}
