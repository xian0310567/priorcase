package health

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// vocabSample 은 경고에 이름을 적어 줄 노트 수다. 전부 적으면 그 줄을 안 읽는다.
const vocabSample = 3

// checkVocabulary 는 **회수에 아무것도 안 더하는 태그**를 센다.
//
// # 왜 이게 검사거리인가
//
// 회수는 `stem + summary + tags` 만 본다. 태그의 낱말이 이미 제목이나 요약에
// 있으면 그 태그를 달든 안 달든 걸리는 질의가 똑같다 — 적는 사람은 회수 어휘를
// 넓혔다고 믿는데 실제로는 아무 일도 안 일어난다. **조용하다.**
//
// 실볼트 실측(2026-08-23): 태그 달린 결정 노트 278건 중 12건(4%)이 그 상태였고,
// 태그가 더하는 새 낱말은 **중앙값 2개**였다. 규약은 "회수 키워드 6~10개, 동의어와
// 상위어를 같이" 를 요구한다 — 헛도는 노트보다 이 중앙값이 더 아픈 숫자다.
//
// 그 대가를 같은 날 재현했다. "웹소설을 AI로 여러 편 찍어내면 경쟁력이 있나" 는
// 관련 규칙을 찾는데, **같은 질문을 바꿔 말한** "작품을 많이 만들어서 승부하면
// 되나" 는 볼트 전체에서 0건이었다. 개념은 있는데 그 낱말이 없다.
//
// # 태그가 없는 노트는 안 센다
//
// 규약이 태그를 강제하지 않는다. 없는 것까지 걸면 옛 노트 전부가 매번 뜨고,
// 늘 뜨는 경고는 무시하는 법을 가르친다. **달았는데 헛도는 것**만 본다.
func checkVocabulary(r *Report, notes []store.Note) {
	var dead []store.Note
	tagged := 0
	for _, n := range notes {
		fresh, gone := search.TagVocabulary(n)
		if len(fresh) == 0 && len(gone) == 0 {
			continue // 태그가 없다 — 이 검사의 대상이 아니다
		}
		tagged++
		if len(fresh) == 0 {
			dead = append(dead, n)
		}
	}
	if tagged == 0 {
		return // 볼트에 태그가 하나도 없다. 할 말이 없다.
	}
	if len(dead) == 0 {
		r.add("회수 어휘", OK,
			fmt.Sprintf("태그 달린 %d건이 전부 새 낱말을 더한다", tagged), "")
		return
	}

	// **최근 것부터 보여 준다.** 옛 노트는 사실상 못 고치지만 방금 쓴 것은 고친다 —
	// 이 경고의 목적은 과거 청소가 아니라 쓰는 습관을 바꾸는 것이다.
	sort.SliceStable(dead, func(i, j int) bool { return dead[i].Meta.Date > dead[j].Meta.Date })
	names := make([]string, 0, vocabSample)
	for _, n := range dead[:min(vocabSample, len(dead))] {
		names = append(names, n.Stem)
	}
	more := ""
	if len(dead) > len(names) {
		more = fmt.Sprintf(" 외 %d건", len(dead)-len(names))
	}
	r.add("회수 어휘", Warn,
		fmt.Sprintf("%d/%d건의 태그가 제목·요약에 이미 있는 낱말뿐이라 회수에 아무것도 더하지 않는다 — %s%s",
			len(dead), tagged, strings.Join(names, " · "), more),
		"그 노트의 tags 에 **동의어와 상위어**를 넣어라 (다시 찾을 때 쓸 낱말이다). "+
			"파일을 고치고 prior index — prior review 로는 태그를 못 고친다")
}
