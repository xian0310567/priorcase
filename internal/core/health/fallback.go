package health

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ── 폴백 도메인에 쌓이는 프로젝트를 찾는다 ──────────────────────────────
//
// **`checkUndeclared` 의 거울상이다.** 그쪽은 "볼트에 폴더가 있는데 설정에 없다"
// 를 잡고, 여기는 **"프로젝트는 있는데 폴더도 설정도 없다"** 를 잡는다. 후자는
// 에러도 경고도 안 난다 — `DomainForCwd` 가 `default_domain` 을 돌려주므로 기록은
// 정상으로 끝나고, 그 자리가 맞는지 아무도 안 본다.
//
// # 왜 자동으로 잡아야 하는가
//
// 사용자가 이 일을 **손으로** 하고 있었다: "가끔씩 볼트 내 common/ 에 접근해서
// 잘못 쌓이고 있는 문서들이 없는지 확인하고 있는데 이건 너무 번거로운 일이야."
// 볼트가 540건이면 그 점검은 결국 안 하게 되고, 안 하면 그 프로젝트의 결정은
// 영영 남의 폴더에 산다.
//
// 값도 정확히 그만큼 든다. 폴백에 쌓인 결정은 **자기 도메인 가점을 못 받고**
// (weightCwdDomain), 그 프로젝트에서 `--project` 로 좁혀도 안 나오며, 폴백을
// 도메인으로 갖는 다른 프로젝트의 회수에는 잡음으로 섞인다.
//
// # 판별자: 안에 여럿 · 밖에 0
//
// 실측(2026-08-31, 실볼트 결정 542건 중 폴백 46건)으로 낱말을 폴백 안팎으로 갈라 셌다:
//
//	token        안   밖
//	twincrew     13    0   ← 프로젝트 이름. 도메인이 없다
//	lg           10    0   ← 같은 프로젝트(LG트윈크루)
//	orca          7   12   ← 도구. 여러 프로젝트를 가로지른다 — 도메인이 아니다
//	shopify       6   16   ← 같음
//	확인           9   27   ← 흔한 낱말
//	lesson       20  169   ← 태그
//
// **밖에 한 건이라도 있으면 프로젝트 이름이 아니다.** 도구·개념·태그는 여러
// 도메인에 걸치고, 프로젝트 이름은 정의상 그 프로젝트에만 있다.
//
// # 임계값
//
// 실볼트 스윕: 2 → 34개(끝까지·나누고·대체 같은 잡음), 3 → 3개(오케스트레이터
// 오탐 1), **5 → 2개(twincrew·lg, 오탐 0)**. 5 를 고른다.
//
// 낮추면 잡음이 곧바로 늘고, 이 경고는 사람이 보고 폴더를 만드는 것이라
// **한 번 잡음이 섞이면 다음부터 안 읽힌다.**
const minFallbackCluster = 5

// FallbackCluster 는 폴백 도메인에만 쌓이는 낱말 하나다.
type FallbackCluster struct {
	Token string
	Count int      // 폴백 도메인 안에서 이 낱말이 걸리는 결정 수
	Stems []string // 예시
}

// FallbackClusters 는 폴백 도메인에 갇힌 프로젝트 후보를 준다.
func FallbackClusters(c *config.Config, l *store.Layout, notes []store.Note) []FallbackCluster {
	fallback := strings.TrimSpace(c.DefaultDomain)
	if fallback == "" {
		return nil // 폴백이 없으면 쌓일 자리도 없다
	}
	declared := map[string]bool{}
	for _, d := range c.Domain {
		declared[strings.ToLower(d.Prefix)] = true
	}
	marker := l.DecisionMarker()
	inside, outside := map[string]int{}, map[string]bool{}
	stems := map[string][]string{}
	for _, n := range notes {
		// **폴백 도메인 하나만 달린 것이 대상이다.** 여러 도메인이 달렸으면
		// 사람이 의도해서 넓힌 것이라 잘못 쌓인 것이 아니다.
		isFallback := len(n.Meta.Domain) == 1 && strings.EqualFold(n.Meta.Domain[0], fallback)
		seen := map[string]bool{}
		for _, k := range search.ExtractKeywords(search.Head(n, marker)) {
			if seen[k] {
				continue
			}
			seen[k] = true
			if isFallback {
				inside[k]++
				stems[k] = append(stems[k], n.Stem)
			} else {
				outside[k] = true
			}
		}
	}
	var out []FallbackCluster
	for k, n := range inside {
		if n < minFallbackCluster || outside[k] || declared[k] {
			continue
		}
		out = append(out, FallbackCluster{Token: k, Count: n, Stems: clip(stems[k])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Token < out[j].Token
	})
	return out
}

func checkFallbackClusters(r *Report, c *config.Config, l *store.Layout, notes []store.Note) {
	const name = "폴백 적체"
	if strings.TrimSpace(c.DefaultDomain) == "" {
		r.add(name, OK, "폴백 도메인이 없다", "")
		return
	}
	cl := FallbackClusters(c, l, notes)
	if len(cl) == 0 {
		r.add(name, OK, "없다", "")
		return
	}
	var parts, fix []string
	for _, x := range cl {
		parts = append(parts, fmt.Sprintf("%s %d건", x.Token, x.Count))
		fix = append(fix, x.Token)
	}
	r.add(name, Warn,
		fmt.Sprintf("%s 가 %s/ 에만 쌓여 있다 — 도메인이 없는 프로젝트다 (예: %s)",
			strings.Join(parts, " · "), c.DefaultDomain, strings.Join(cl[0].Stems, ", ")),
		fmt.Sprintf("설정에 [[domain]] 블록을 만들고 옮겨라: prior domain split %s",
			strings.Join(fix, " ")))
}
