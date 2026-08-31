package search

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **대화체 회수는 실볼트에서만 잴 수 있다.** realvault_test.go 의 § 과 같은 이유다.
//
//	PRIORCASE_TEST_VAULT="$HOME/Documents/Obsidian Vault" \
//	  go test ./internal/core/search -run RealVaultConversational -v
//
// # 이 세트가 재는 것
//
// 기존 정답질의 두 벌(slug·요약뒤쪽)은 **노트 자기 낱말로 질의를 만든다.** 그래서
// 검색은 잘 재지만 2026-08-31 재현 고장은 못 잡는다 — 그 고장은 질의가 노트의
// 낱말 **하나만** 담을 때 나기 때문이다. 실제로 두 세트의 MRR 은 고장이 살아
// 있는 동안에도 0.86·0.96 이었다.
//
// 여기서는 주제어 **하나**를 대화체 껍데기에 넣는다. 사람이 실제로 치는 모양이다:
//
//	"나 이제 뭐 해야하지?? 지라, 슬랙, 구글챗을 확인해서 찾아봐줘"
//
// # @1 을 목표로 삼지 마라
//
// 주제어는 df 2~25 인 것을 고르므로 **같은 주제의 노트가 여럿이다.** 그중 어느
// 것이 나와도 사람에게는 맞는 답인데 이 세트는 특정 한 건만 정답으로 센다.
// 봐야 하는 숫자는 @3(훅이 싣는 칸 수)과 못찾음이다.

// convTemplate 는 실제로 고장을 낸 프롬프트의 형태다.
const convTemplate = "나 이제 뭐 해야하지?? %s 확인해서 찾아봐줘"

// noiseQuery 는 **내용어가 하나도 없는** 질의다. scoreAll 의 옛 게이트 § 이 실측한
// 그 문장이고, 게이트를 너무 풀면 여기서 후보가 튀어나온다.
const noiseQuery = "무슨 작업을 하다가 멈춘것같은데 확인해줄 수 있어"

// convTopic 은 노트의 head 에서 **주제어 하나**를 고른다.
//
// df ≥ 2 인 것 중 가장 드문 것을 쓴다. df=1 은 그 노트에만 있는 문자열이라 사람이
// 그 낱말로 물을 수가 없고, df 가 크면 주제어가 아니다. 실볼트에서 지라(df 11)·
// 슬랙(4)·브라우저(15)가 이 구간이다.
func convTopic(head string, df map[string]int) string {
	best, bestDF := "", 1<<30
	for _, k := range ExtractKeywords(head) {
		d := df[k]
		if d < 2 || d > 25 {
			continue
		}
		if d < bestDF || (d == bestDF && k > best) {
			best, bestDF = k, d
		}
	}
	return best
}

type convCase struct {
	note  store.Note
	query string
}

func convSet(l *store.Layout, notes []store.Note) []convCase {
	heads := make([]string, len(notes))
	for i, n := range notes {
		heads[i] = headOf(l, n)
	}
	df := map[string]int{}
	for _, h := range heads {
		for _, k := range ExtractKeywords(h) {
			if _, ok := df[k]; ok {
				continue
			}
			c := 0
			for _, hh := range heads {
				if matches(hh, k) {
					c++
				}
			}
			df[k] = c
		}
	}
	var out []convCase
	for i, n := range notes {
		if topic := convTopic(heads[i], df); topic != "" {
			out = append(out, convCase{n, fmt.Sprintf(convTemplate, topic)})
		}
	}
	return out
}

// score 는 세트 하나를 돌려 (못찾음, @1, @3, MRR) 을 준다.
func (cs convCase) rank(l *store.Layout, c *config.Config) int {
	hits, _, _ := Recall(l, c, cs.query, Options{CrossProject: true, Limit: 20, MinScore: 1})
	for i, h := range hits {
		if h.Note.Path == cs.note.Path {
			return i + 1
		}
	}
	return 0
}

func runConvSet(l *store.Layout, c *config.Config, set []convCase) (miss, at1, at3 int, mrr float64) {
	for _, cs := range set {
		switch r := cs.rank(l, c); {
		case r == 0:
			miss++
		default:
			mrr += 1 / float64(r)
			if r == 1 {
				at1++
			}
			if r <= 3 {
				at3++
			}
		}
	}
	return miss, at1, at3, mrr / float64(len(set))
}

func TestRealVaultConversationalRecall(t *testing.T) {
	vault := os.Getenv("PRIORCASE_TEST_VAULT")
	if vault == "" {
		t.Skip("PRIORCASE_TEST_VAULT 가 없다 — 대화체 측정은 로컬 전용이다")
	}
	c := realVaultConfig(t, vault)
	l := store.NewLayout(c)
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	set := convSet(l, notes)
	if len(set) == 0 {
		t.Skip("주제어를 뽑을 수 있는 노트가 없다")
	}
	miss, at1, at3, mrr := runConvSet(l, c, set)
	n := float64(len(set))
	t.Logf("대화체 단일주제 n=%d: 못찾음 %d(%.0f%%) · @1 %d(%.0f%%) · @3 %d(%.0f%%) · MRR %.3f",
		len(set), miss, 100*float64(miss)/n, at1, 100*float64(at1)/n,
		at3, 100*float64(at3)/n, mrr)

	// ── 회귀 가드 ────────────────────────────────────────────────────
	//
	// 2026-08-31 실측(결정 542건): 못찾음 2% · @3 97% · MRR 0.751.
	// 고치기 전 값은 못찾음 89% · @3 9% · MRR 0.065 였다. 바닥은 넉넉히 둔다 —
	// 이 가드가 잡으려는 것은 미세한 변동이 아니라 **게이트나 불용어를 만지다
	// 이 고장을 되살린 판**이다.
	if got := 100 * float64(miss) / n; got > 25 {
		t.Errorf("대화체 질의의 %.0f%% 가 정답을 못 찾는다 (기준 25%%) — "+
			"주제 하나를 대화체로 물었을 때 그 노트가 안 나온다", got)
	}
	if mrr < 0.45 {
		t.Errorf("대화체 MRR %.3f < 0.45 — 찾기는 하는데 상위에 못 든다", mrr)
	}

	// ── 잡음 가드 ────────────────────────────────────────────────────
	//
	// 내용어 없는 질의는 **침묵해야 한다.** 실볼트에서 rareDFRatio 를 0.05 로
	// 올리면 `작업`(df 4.1%)이 변별어가 되어 여기서 후보 22건이 돌아온다.
	if hits, _, _ := Recall(l, c, noiseQuery, Options{
		CrossProject: true, Limit: 200, MinScore: 1,
	}); len(hits) > 3 {
		t.Errorf("내용어 없는 질의가 후보 %d건을 만든다 — 게이트를 너무 풀었다", len(hits))
	}
}

// TestRealVaultGateSweep 은 **rareDFRatio·weightRare 를 다시 고르는 자리**다.
//
// 볼트가 커지면 값을 다시 재야 한다(refHeadRunes 와 같은 성격이다). 판정 없이
// 표만 찍는다 — 어느 값을 고를지는 사람이 본다.
//
// 2026-08-31 실측 (결정 542건):
//
//	ratio  wRare   못찾음    @1     @3    MRR    잡음후보
//	0.00   0        88%    3%     9%   0.065      0     ← 옛 게이트 (변별어 없음)
//	0.02   0         2%    3%    44%   0.299      0
//	0.03   0         2%    3%    44%   0.299      0
//	0.03   2         2%    3%    56%   0.330      0     ← 시간부사 불용어 넣기 전
//	0.05   2         2%    3%    56%   0.330     22     ← `작업`이 변별어가 된다
//	0.08   2         1%    3%    56%   0.330     22
//
// 위쪽 한계는 잡음 후보가 정한다(0.03 → 0건, 0.05 → 22건). 아래쪽은 0.02 미만에서
// 게이트가 안 풀린다. weightRare 는 1·2·3 이 MRR 0.329·0.330·0.331 로 사실상
// 같아 가운데를 골랐다.
func TestRealVaultGateSweep(t *testing.T) {
	vault := os.Getenv("PRIORCASE_TEST_VAULT")
	if vault == "" {
		t.Skip("PRIORCASE_TEST_VAULT 가 없다 — 스윕은 로컬 전용이다")
	}
	if os.Getenv("PRIORCASE_SWEEP") == "" {
		t.Skip("PRIORCASE_SWEEP 이 없다 — 상수를 다시 고를 때만 돌린다 (2~3분)")
	}
	c := realVaultConfig(t, vault)
	l := store.NewLayout(c)
	notes, _, _ := l.List()
	set := convSet(l, notes)

	origRatio, origW := rareDFRatio, weightRare
	defer func() { rareDFRatio, weightRare = origRatio, origW }()

	var b strings.Builder
	fmt.Fprintf(&b, "\n%-7s %-6s %8s %6s %6s %7s %9s\n",
		"ratio", "wRare", "못찾음", "@1", "@3", "MRR", "잡음후보")
	for _, cf := range []struct {
		ratio float64
		wRare int
	}{{0.00, 0}, {0.02, 0}, {0.03, 0}, {0.03, 1}, {0.03, 2}, {0.03, 3}, {0.05, 2}, {0.08, 2}} {
		rareDFRatio, weightRare = cf.ratio, cf.wRare
		miss, at1, at3, mrr := runConvSet(l, c, set)
		noise, _, _ := Recall(l, c, noiseQuery, Options{CrossProject: true, Limit: 200, MinScore: 1})
		n := float64(len(set))
		fmt.Fprintf(&b, "%-7.2f %-6d %7.0f%% %5.0f%% %5.0f%% %7.3f %9d\n",
			cf.ratio, cf.wRare, 100*float64(miss)/n, 100*float64(at1)/n,
			100*float64(at3)/n, mrr, len(noise))
	}
	t.Log(b.String())
}
