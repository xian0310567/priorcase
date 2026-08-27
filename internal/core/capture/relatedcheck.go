package capture

import (
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// related 대상이 실재하는지 본다 — **쓰기 시점에.**
//
// # 왜 필요한가 (비대칭이 원인이었다)
//
// 2026-08-27 실볼트 403노트에서 깨진 참조 19건이 나왔는데 **전부 `related` 였고
// `supersedes` 는 한 건도 없었다.** 우연이 아니다:
//
//	supersedes → supersedeOne 이 l.ResolveStem 을 부른다. 역방향 링크를 쓰려면
//	             대상을 열어야 하므로 실재하지 않으면 애초에 에러다.
//	related    → normalizeLinks 가 store.NormalizeLink 로 **형식만** 손본다.
//	             `[[ ]]` 를 씌우고 경로 순회를 막을 뿐, 실재 여부는 안 본다.
//
// 그래서 에이전트가 이름을 기억으로 타이핑하면 그대로 frontmatter 에 안착하고,
// 며칠 뒤 `prior doctor` 가 잡을 때까지 썩는다. 실제로 3일 만에 19건이 쌓였다.
//
// 오타 양상이 그 진단을 뒷받침한다 — `봌라우잠`(브라우저), `업스테이지직`(측),
// `추족으로`(충족), `바이드전달`(빌드전달). 그리고 오타가 아닌 것도 하나 있었다:
// `…보여지는-버전을뒤집어…` 의 실제 이름은 `…빌드기본값을뒤집어…` 였다.
// **뜻만 기억하고 이름을 지어낸 것**이라, 형식 검사로는 절대 못 잡는다.
//
// # 왜 볼트 전체 이름으로 보는가
//
// `related` 는 결정이 아닌 문서도 정당하게 가리킨다 — 프로젝트 개요, 볼트 네이밍
// 규약, 플레이북. `ResolveStem`(결정 폴더만 본다)으로 검사하면 그것들이 전부
// 거짓 양성이 된다. 2026-08-24 에 같은 실수를 한 번 했다: 결정 노트만 대조해
// 깨진 링크 75건을 보고했는데 실제로는 18건이었다.
//
// 그래서 `l.AllStems()` 를 쓴다 — `health.checkLinks` 가 쓰는 것과 **같은 근거**다.
// 두 자리가 다른 기준을 쓰면 "capture 는 받았는데 doctor 는 깬 링크라고 한다" 가 된다.
//
// # 왜 거절하지 않고 빼는가
//
// `supersedes` 는 에러로 막는다. 그건 뒤집기가 반쪽으로 남으면 두 노트가 다 사실과
// 달라지기 때문이다. `related` 는 다르다 — 장식에 가깝고, 그것 하나 때문에 기록
// 전체를 실패시키면 **결정이 통째로 사라질 수 있다.** 이 프로젝트가 태그를 강제하지
// 않기로 한 것과 같은 이유다: "기록이 귀찮아지면 안 남긴다."
//
// 그래서 **빼고, 크게 알린다.** 빼면 썩지 않고, 알리면 고칠 수 있다.
// 자동 교정은 하지 않는다 — **잘못 이은 링크는 끊긴 링크보다 나쁘다.** 끊긴 것은
// 회색으로 보이지만 잘못 이은 것은 아무도 의심하지 않는다.

// DroppedLink 는 대상이 없어서 빼 버린 related 값이다.
//
// Suggest 가 비어 있지 않으면 볼트에서 가장 가까운 이름이다. **제안일 뿐 교정이
// 아니다** — 맞는지는 사람이나 에이전트가 판단한다.
type DroppedLink struct {
	Value   string
	Suggest string
}

// resolveRelated 는 related 값을 정규화하고, 대상이 없는 것을 빼서 따로 준다.
//
// 형식 위반(경로 순회 등)은 여전히 에러다 — 그건 오타가 아니라 공격 표면이다.
func resolveRelated(l *store.Layout, raw []string) ([]string, []DroppedLink, error) {
	links, err := normalizeLinks(raw)
	if err != nil || len(links) == 0 {
		return links, nil, err
	}
	stems, serr := l.AllStems()
	if serr != nil {
		// **볼트를 못 훑는 것으로 기록을 막지 않는다.** 검사가 안 될 뿐이고,
		// 그때는 doctor 가 나중에 잡는다. 원래 동작으로 떨어진다.
		return links, nil, nil
	}
	var out []string
	var dropped []DroppedLink
	for _, link := range links {
		stem := strings.TrimSpace(strings.Trim(link, "[]"))
		if stem == "" || stems[store.NFC(stem)] {
			out = append(out, link)
			continue
		}
		dropped = append(dropped, DroppedLink{Value: stem, Suggest: store.NearestStem(stem, stems)})
	}
	return out, dropped, nil
}
