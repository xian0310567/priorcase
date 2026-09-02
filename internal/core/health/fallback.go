package health

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// Token 은 **세는 데 쓰는 키**다. search.ExtractKeywords 가 준 검색용 토큰이라
	// 한국어 조사가 떨어져 있을 수 있다 — 매칭에는 그게 맞고, 임계값 실측(5)도
	// 이 토큰 기준이다. **이름으로 쓰지 마라.** Name() 을 써라.
	Token string
	Count int      // 폴백 도메인 안에서 이 낱말이 걸리는 결정 수
	Stems []string // 예시

	// surface 는 본문에 실제로 나타난 표기 중 가장 흔한 것이다 (Name 의 §).
	surface string
}

// Name 은 **사람에게 보여 주고 도메인 접두어로 제안할 이름**이다.
//
// # 왜 Token 을 그대로 쓰면 안 되는가 (2026-09-02 실볼트)
//
// `젠틀파이`(회사 이름)가 common/ 에 9건 쌓였는데 doctor 가 제안한 것은 `젠틀파`
// 였다. `prior domain split 젠틀파` 를 그대로 실행하면 결정 9건이 `젠틀파-결정-…`
// 으로 개명된다 — **회사 이름 오타가 파일명에 영구히 박힌다.**
//
// Token 은 `search.ExtractKeywords` 의 결과라 한국어 조사가 떨어져 있다.
// `젠틀파이` 는 끝의 `이` 가 조사로 보여 잘린다. keywords.go 의 원형 복구 가드는
// 2글자 미만일 때만 돌고 `젠틀파` 는 3글자라 빠져나간다 — 그 주석이 예로 든
// `파이(→파)` 가 복합어 안에서 일어난 판이다.
//
// # 왜 '가장 긴 것' 이 아니라 '가장 흔한 것' 인가
//
// 같은 볼트에 `젠틀파이서버` 도 있다. 접두사가 가장 긴 것을 고르면 그게 뽑히는데,
// 그건 프로젝트 이름이 아니라 그 프로젝트의 서버 이야기다. 빈도로 고르면
// `젠틀파이`(여러 건)가 `젠틀파이서버`(한 건)를 이긴다.
//
// 같은 빈도면 짧은 쪽이다 — 도메인 접두어는 파일명에 들어가므로 짧을수록 낫고,
// 무엇보다 **결정적이어야** 한다(같은 볼트에서 매번 같은 제안이 나와야 한다).
func (f FallbackCluster) Name() string {
	if f.surface != "" {
		return f.surface
	}
	return f.Token
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
	// 토큰마다 **본문에 실제로 나타난 표기**를 세어 둔다 (FallbackCluster.Name 의 §).
	surfaces := map[string]map[string]int{}
	for _, n := range notes {
		// **폴백 도메인 하나만 달린 것이 대상이다.** 여러 도메인이 달렸으면
		// 사람이 의도해서 넓힌 것이라 잘못 쌓인 것이 아니다.
		isFallback := len(n.Meta.Domain) == 1 && strings.EqualFold(n.Meta.Domain[0], fallback)
		head := search.Head(n, marker)
		seen := map[string]bool{}
		for _, k := range search.ExtractKeywords(head) {
			if seen[k] {
				continue
			}
			seen[k] = true
			if isFallback {
				inside[k]++
				stems[k] = append(stems[k], n.Stem)
				if s := surfaceOf(head, k); s != "" {
					if surfaces[k] == nil {
						surfaces[k] = map[string]int{}
					}
					surfaces[k][s]++
				}
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
		out = append(out, FallbackCluster{
			Token: k, Count: n, Stems: clip(stems[k]), surface: pickSurface(surfaces[k]),
		})
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
	// **Token 이 아니라 Name() 이다** (FallbackCluster.Name 의 §). 여기서 Token 을
	// 쓰면 조사에 잘린 이름이 그대로 제안되고, 사람이 그 제안을 복사해 실행하면
	// 오타가 파일명에 영구히 박힌다.
	var parts, fix []string
	for _, x := range cl {
		parts = append(parts, fmt.Sprintf("%s %d건", x.Name(), x.Count))
		fix = append(fix, x.Name())
	}
	r.add(name, Warn,
		fmt.Sprintf("%s이 %s/ 에만 쌓여 있다 — 도메인이 없는 프로젝트다 (예: %s)",
			strings.Join(parts, " · "), c.DefaultDomain, strings.Join(cl[0].Stems, ", ")),
		fmt.Sprintf("설정에 [[domain]] 블록을 만들고 옮겨라: prior domain split %s",
			strings.Join(fix, " ")))
}

// surfaceOf 는 head 에서 keyword 로 시작하는 낱말을 찾아 그 **원래 표기**를 준다.
//
// 토큰이 조사에 잘렸을 때 원형을 되찾는 자리다. 낱말 경계는 회수와 같은 자를 쓰지
// 않는다 — 여기서 필요한 것은 "이 토큰이 어느 낱말의 앞부분인가" 이고, 그 낱말은
// 하이픈·공백·문장부호로 끊긴다(파일명 stem 이 하이픈으로 이어져 있다).
func surfaceOf(head, keyword string) string {
	if keyword == "" {
		return ""
	}
	low := strings.ToLower(head)
	best := ""
	for i := 0; ; {
		j := strings.Index(low[i:], keyword)
		if j < 0 {
			break
		}
		j += i
		i = j + len(keyword)
		// 낱말 **앞**이 경계여야 한다. 안 그러면 `젠틀파` 가 `초젠틀파이` 에도 걸린다.
		if j > 0 && !isNameBoundary(rune(low[j-1])) {
			continue
		}
		end := j + len(keyword)
		for end < len(head) {
			r, w := utf8.DecodeRuneInString(head[end:])
			if isNameBoundary(r) {
				break
			}
			end += w
		}
		w := head[j:end]
		// 가장 짧은 것을 고른다 — 접미가 붙은 것(`젠틀파이서버`)보다 원형이 낫고,
		// 빈도 집계는 pickSurface 가 따로 한다.
		if best == "" || len([]rune(w)) < len([]rune(best)) {
			best = w
		}
	}
	return best
}

// isNameBoundary 는 낱말 이름이 끊기는 자리인지 본다. 파일명 stem 이 하이픈으로
// 이어져 있으므로 하이픈도 경계다.
func isNameBoundary(r rune) bool {
	if r == '-' || r == '_' || r == '.' || r == '/' {
		return true
	}
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// pickSurface 는 가장 흔한 표기를 고른다. 같으면 짧은 쪽, 그것도 같으면 사전순 —
// **같은 볼트에서 매번 같은 제안이 나와야** 사람이 그 제안을 믿는다.
func pickSurface(counts map[string]int) string {
	best, bestN := "", 0
	for s, n := range counts {
		switch {
		case n > bestN:
			best, bestN = s, n
		case n == bestN && best != "":
			if a, b := len([]rune(s)), len([]rune(best)); a < b || (a == b && s < best) {
				best = s
			}
		}
	}
	return best
}
