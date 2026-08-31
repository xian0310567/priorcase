// Package retro 는 **결과를 물어볼 때가 된 결정**을 골라낸다.
//
// 이 패키지가 있는 이유는 실측이다. 결정 노트 96건 중 `outcome` 이 판명된 것은
// 2건(2%)뿐이었고, 회고 절이 채워진 것은 34건이었다. `prior review --outcome good`
// 이라는 기능은 처음부터 있었다 — 안 쓰인 이유는 **되돌아볼 순간을 아무도 만들어
// 주지 않아서**다. 기록은 훅이 자동으로 시키는데 회고는 시키는 사람이 없다.
//
// 그래서 "언제가 그때인가" 를 여기서 정한다.
package retro

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Reason 은 이 결정을 왜 지금 묻는지다. 사람에게 그대로 보여 준다 —
// 이유 없이 묻는 큐는 소음이고, 소음은 사람에게 무시하는 법을 가르친다.
type Reason string

const (
	// ReasonRecalled 는 이 결정이 나중 결정을 기록할 때 다시 꺼내졌다는 뜻이다.
	ReasonRecalled Reason = "recalled"
	// ReasonSuperseded 는 이 결정을 뒤집는 결정이 나왔다는 뜻이다.
	ReasonSuperseded Reason = "superseded"
)

// Item 은 회고 큐의 한 줄이다.
type Item struct {
	Stem   string `json:"stem"`
	Date   string `json:"date"`
	Domain string `json:"domain"`
	// Vault 는 이 결정이 사는 볼트의 이름이다.
	//
	// **앱이 이걸 보여 줘야 한다.** 메뉴바 앱에는 cwd 가 없어서 큐가 볼트를 전부
	// 덮는데, 줄에 볼트가 안 붙으면 사람이 "이게 어디 결정이지" 를 물을 수 없다.
	Vault   string `json:"vault"`
	Summary string `json:"summary"`
	Author  string `json:"author,omitempty"`
	Reason  Reason `json:"reason"`
	// Hits 는 재회수된 횟수다. ReasonSuperseded 만으로 올라온 것은 0 일 수 있다.
	Hits int `json:"hits"`
}

// MinHits 는 재회수가 몇 번부터 방아쇠인가다.
//
// **1이 아니라 2인 이유가 실측이다.** 볼트 96건으로 시뮬레이션했더니 한 번이라도
// 재회수된 노트가 45건(47%)이었고 재회수 횟수의 **중앙값이 1회**였다. 즉 대부분은
// 한 번 스쳤을 뿐이고, 그걸 "회고할 때가 됐다" 로 보는 것은 과하다.
//
// 2로 자르면 43 → 26건, 하루 3.9 → 2.4건이 된다. 그리고 남는 것이 실제로 옳다 —
// 이 프로젝트가 반복해서 되돌아본 결정들(수익모델·포지셔닝·구현스택)이 그대로 올라왔다.
const MinHits = 2

// recallLimit·recallMinScore 는 재현할 편승 회수의 조건이다.
//
// **`prior capture` 가 쓰는 값과 같아야 한다.** 이 큐가 재현하는 것은 "그 결정을
// 기록하던 순간 무엇이 함께 보였나" 이고, 값이 갈리면 재현이 아니라 다른 계산이 된다.
// arch 테스트가 두 자리를 묶어 둔다.
const (
	recallLimit    = 3
	recallMinScore = 1
)

// Due 는 지금 물어볼 결정을 준다. 최근 것이 먼저다.
//
// **최소 경과 기간을 두지 않는다.** 기간을 얹으면 규칙이 둘이 되고 "왜 안 뜨지" 를
// 설명하기 어려워진다. 근거가 이미 그 역할을 한다 — 하루 만에 두 번 재회수되는
// 결정은 실제로 그날 계속 참조된 것이고, 그런 결정은 물어볼 값이 있다.
//
// **매번 계산한다. 캐시하지 않는다.** 상태를 하나 더 두면 볼트를 손으로 고쳤을 때
// 그것이 틀어진다 — 색인이 이미 그 문제를 겪었다.
// AllDue 는 **선언된 볼트를 전부** 돌아 회고 큐를 만든다.
//
// # 왜 필요한가
//
// Due 는 Layout 하나 = 볼트 하나를 본다. CLI 와 훅은 cwd 로 볼트를 고르므로 그것이
// 맞지만, **메뉴바 앱에는 cwd 가 없다** — 셸이 마지막으로 있던 자리의 볼트를 보게
// 되고 사람은 그걸 예측할 수 없다. 실측으로 같은 설정에서 cwd 만 바꿔 부르니
// 회고 큐가 0건과 43건으로 갈렸다.
//
// # 볼트를 넘지 않는다
//
// 볼트마다 Due 를 따로 부르고 결과를 잇는다. **한 볼트의 노트가 다른 볼트에서
// 회수되는 일은 없다** — 회수의 볼트 경계는 그대로다. 넘는 것은 "무엇을 회고할
// 때가 됐나" 라는 목록뿐이고, 그건 감독이지 회수가 아니다.
//
// 한 볼트가 실패해도 나머지를 준다. 다만 **몇 개를 못 봤는지 알린다** — 조용히
// 짧은 목록을 주면 "회고할 것이 없다" 로 보인다.
func AllDue(c *config.Config) ([]Item, []store.SkippedNote, []error) {
	var all []Item
	var skipped []store.SkippedNote
	var errs []error
	for _, v := range c.Vaults {
		// **볼트가 통째로 없으면 List 는 조용히 빈 목록을 준다.** 결정 폴더를
		// glob 으로 찾는데 볼트 디렉토리가 없으면 매칭이 0건일 뿐 에러가 아니다.
		// 그러면 "회고할 것이 없다" 와 "볼트를 못 읽었다" 가 같아 보인다 —
		// 이 프로젝트가 죄목으로 드는 바로 그 실패다.
		if fi, err := os.Stat(v.Path); err != nil {
			errs = append(errs, fmt.Errorf("볼트 %s (%s) 에 접근할 수 없다: %w", v.Name, v.Path, err))
			continue
		} else if !fi.IsDir() {
			errs = append(errs, fmt.Errorf("볼트 %s (%s) 가 디렉토리가 아니다", v.Name, v.Path))
			continue
		}
		items, sk, err := Due(store.NewLayoutFor(c, v), c)
		if err != nil {
			errs = append(errs, fmt.Errorf("볼트 %s: %w", v.Name, err))
			continue
		}
		for i := range items {
			items[i].Vault = v.Name
		}
		all = append(all, items...)
		skipped = append(skipped, sk...)
	}
	// 날짜순으로 세운다 — 볼트별로 뭉쳐 있으면 사람이 "오래된 것부터" 를 못 본다.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Date != all[j].Date {
			return all[i].Date < all[j].Date
		}
		return all[i].Stem < all[j].Stem
	})
	return all, skipped, errs
}

func Due(l *store.Layout, c *config.Config) ([]Item, []store.SkippedNote, error) {
	// **볼트를 한 번만 읽는다.**
	//
	// 아래 루프가 노트마다 Recall 을 부르는데, Recall 은 기본적으로 부를 때마다
	// 볼트 전체를 디스크에서 다시 읽는다. 그러면 비용이 노트 수의 제곱이 된다 —
	// 실측으로 결정 156건에서 2.6초이던 `prior queue` 가 558건에서 32초가 됐고,
	// 앱의 읽기 상한이 10초라 화면이 통째로 오류로 찼다 (search.Corpus 의 §).
	//
	// **기능이 늘어서가 아니라 볼트가 자라서** 죽는 종류라, 아무것도 안 고쳐도
	// 어느 날 갑자기 그렇게 된다.
	corpus, err := search.LoadCorpus(l)
	if err != nil {
		return nil, nil, err
	}
	notes, skipped := corpus.Notes, corpus.Skipped

	// 날짜순으로 세운다. "나중 결정이 이전 결정을 꺼냈나" 를 보려면 순서가 필요하다.
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Meta.Date != notes[j].Meta.Date {
			return notes[i].Meta.Date < notes[j].Meta.Date
		}
		return notes[i].Stem < notes[j].Stem
	})

	byStem := make(map[string]store.Note, len(notes))
	for _, n := range notes {
		byStem[n.Stem] = n
	}

	hits := map[string]int{}
	seen := map[string]bool{}
	for _, n := range notes {
		// 이 노트가 기록되던 순간의 편승 회수를 재현한다. 그때 나온 **이전** 노트가
		// "다시 꺼내진" 것이다.
		//
		// **질의도 옵션도 capture 와 같아야 한다.**
		//   - 질의는 `summary + " " + slug` 다 (capture.Do 참고)
		//   - **Cwd 를 주지 않는다.** 주면 그 도메인에 +4 가 붙어 다른 프로젝트의
		//     결정이 상위 3에서 밀려난다. 실측으로 그 차이를 봤다 — cwd 를 준
		//     계산은 큐를 26건으로, 안 준 계산은 33건으로 냈고 빠진 7건이 전부
		//     다른 프로젝트 것이었다. capture 는 Cwd 를 주지 않으므로 이쪽이 맞다.
		found, _, err := search.Recall(l, c, n.Meta.Summary+" "+slugOf(l, n.Stem),
			search.Options{
				Limit: recallLimit, MinScore: recallMinScore, CrossProject: true,
				Corpus: corpus,
			})
		if err != nil {
			// 한 건이 실패해도 큐 전체를 버리지 않는다. 회고는 놓쳐도 대화를 막지 않는
			// 종류의 일이고, 여기서 에러를 올리면 앱의 다른 큐까지 같이 죽는다.
			continue
		}
		for _, h := range found {
			if h.Note.Stem == n.Stem || !seen[h.Note.Stem] {
				continue // 자기 자신이거나, 아직 안 나온 = 미래의 노트
			}
			hits[h.Note.Stem]++
		}
		seen[n.Stem] = true
	}

	// 뒤집힌 결정을 모은다. 재회수가 0회여도 판정할 것이 있다 — 오히려 가장 확실한
	// 경우다. 결과가 이미 나왔고 그것이 "뒤집혔다" 는 사실 자체다.
	superseded := map[string]bool{}
	for _, n := range notes {
		if n.Meta.Status == "superseded" {
			superseded[n.Stem] = true
		}
	}

	var out []Item
	for stem, n := range byStem {
		// **결과가 이미 판명된 것은 묻지 않는다.** 이 조건이 없으면 사람이 답한
		// 결정이 매번 다시 올라와, 답할수록 큐가 안 줄어드는 것처럼 보인다.
		if n.Meta.Outcome != "pending" {
			continue
		}
		switch {
		case hits[stem] >= MinHits:
			out = append(out, item(n, ReasonRecalled, hits[stem]))
		case superseded[stem]:
			out = append(out, item(n, ReasonSuperseded, hits[stem]))
		}
	}

	// 재회수가 많은 것이 먼저, 같으면 최근 것이 먼저. 자주 참조되는 결정일수록
	// 결과를 아는 것의 값이 크다.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Stem < out[j].Stem
	})
	return out, skipped, nil
}

// slugOf 는 stem 에서 slug 를 뽑는다. stem 은 `{domain}{marker}{slug}-{date}` 다.
//
// capture 의 편승 회수가 `summary + " " + slug` 로 질의하므로 재현에 필요하다.
// 못 뽑으면 빈 문자열을 준다 — 질의가 summary 만 남을 뿐 큐가 죽지는 않는다.
func slugOf(l *store.Layout, stem string) string {
	i := strings.Index(stem, l.DecisionMarker())
	if i < 0 {
		return ""
	}
	rest := stem[i+len(l.DecisionMarker()):]
	j := strings.LastIndex(rest, "-")
	if j < 0 {
		return rest
	}
	return rest[:j] // 뒤의 -{date} 를 뗀다
}

func item(n store.Note, r Reason, hits int) Item {
	d := ""
	if len(n.Meta.Domain) > 0 {
		d = n.Meta.Domain[0]
	}
	return Item{
		Stem: n.Stem, Date: n.Meta.Date, Domain: d,
		Summary: n.Meta.Summary, Author: n.Meta.Author,
		Reason: r, Hits: hits,
	}
}
