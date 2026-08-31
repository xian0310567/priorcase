package search

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **점수식 상수는 실볼트에서만 잴 수 있다.**
//
// `refHeadRunes`·`normB`·`weightSynonym`·`penaltySuperseded` 는 전부 실볼트 측정으로
// 골랐고, 볼트가 커지면 다시 재야 한다. 그런데 실볼트 사본은 저장소에 넣을 수 없다
// (결정 노트에 개인 내용이 있다). 그래서 **로컬 전용 테스트**로 둔다 — CI 에서는
// 환경변수가 없으므로 건너뛴다.
//
//	PRIORCASE_TEST_VAULT="$HOME/Documents/Obsidian Vault" \
//	  go test ./internal/core/search -run RealVault -v
//
// # 프롬프트 세트를 같이 주면 편향까지 잰다
//
// **합성 질의로는 길이 편향이 재현되지 않는다.** 고장의 원인이 대화체 프롬프트의
// 우연한 부분문자열 히트라서, 사람이 골라 넣은 질의로만 재면 안 보인다. 그래서
// 실제 프롬프트를 파일로 받는다 — `[{"cwd": "...", "prompt": "..."}, ...]` JSON 이다.
//
//	PRIORCASE_MEASURE_PROMPTS=/tmp/prompts.json
//
// Claude Code 트랜스크립트에서 뽑는 법: `~/.claude/projects/*/*.jsonl` 의 각 줄에서
// `type == "user"` 이고 `promptSource == "typed"` 인 것의 `cwd` 와 `message.content`.
// (`promptSource` 가 없는 줄은 훅 출력·슬래시 명령이라 사람이 친 것이 아니다.)
//
// # 볼트를 읽기만 한다
//
// 도메인은 설정에서 가져오지 않고 **폴더 구조에서 유도한다** — `decisions/` 하위를
// 가진 최상위 폴더가 도메인이다. 사용자 설정에 의존하면 그 머신에 선언되지 않은
// 도메인이 조용히 빠진 채로 측정된다(2026-08-21 에 실제로 그랬다: 결정 217건이
// 회수에서 빠져 있었다).
func TestRealVaultRecallBias(t *testing.T) {
	vault := os.Getenv("PRIORCASE_TEST_VAULT")
	if vault == "" {
		t.Skip("PRIORCASE_TEST_VAULT 가 없다 — 실볼트 측정은 로컬 전용이다")
	}
	c := realVaultConfig(t, vault)
	l := store.NewLayout(c)

	notes, skipped, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) > 0 {
		for _, s := range skipped {
			t.Errorf("읽지 못한 결정 노트: %s — %v", l.RelPath(s.Path), s.Reason)
		}
	}
	if len(notes) == 0 {
		t.Fatal("결정 노트가 없다 — 볼트 경로나 폴더 구조를 보라")
	}
	rules, ruleSkipped, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("볼트: 결정 %d건 · 규칙 %d건 (읽지 못함 결정 %d · 규칙 %d)",
		len(notes), len(rules), len(skipped), len(ruleSkipped))

	// ── head 길이 분포 ─────────────────────────────────────────────────
	//
	// refHeadRunes 의 근거가 이 분포다. 중앙값이 크게 움직였으면 그 상수를 다시 골라야
	// 한다 — 지금 값(200)은 2026-08-27 의 중앙 184 · p75 263 을 보고 정한 것이다.
	lens := make([]int, 0, len(notes))
	byPath := map[string]int{}
	for _, n := range notes {
		hl := len([]rune(headOf(l, n)))
		lens = append(lens, hl)
		byPath[n.Path] = hl
	}
	sort.Ints(lens)
	t.Logf("head 글자수: 최소 %d · p25 %d · 중앙 %d · p75 %d · p90 %d · 최대 %d (refHeadRunes=%d)",
		lens[0], pct(lens, .25), pct(lens, .50), pct(lens, .75), pct(lens, .90),
		lens[len(lens)-1], refHeadRunes)

	// ── 정답을 아는 질의 ──────────────────────────────────────────────
	//
	// 두 벌을 쓴다. `slug` 는 파일명에서 뽑아 **길이에 중립**이고, `요약뒤쪽` 은 요약의
	// 마지막 1/3 에서 뽑아 **긴 요약의 꼬리가 살아 있는지** 본다. 후자가 있어야
	// "요약을 N자로 잘라 head 를 만드는" 안을 기각할 근거가 생긴다.
	slugMRR := knownAnswers(t, l, c, notes, "slug", slugQuery)
	tailMRR := knownAnswers(t, l, c, notes, "요약뒤쪽", func(n store.Note) string {
		return tailQuery(n.Meta.Summary)
	})

	// **회귀 가드.** 상수를 만지다가 정확도를 깨는 것을 막는다. 실측 기준선은
	// slug 0.922 · 요약뒤쪽 0.952 (2026-08-27, 결정 414건)이고, 볼트가 커지면
	// 자연히 내려가므로 바닥은 넉넉히 둔다 — 이 가드가 잡으려는 것은 미세한 변동이
	// 아니라 "상수를 잘못 만져서 회수가 무너진" 판이다.
	const mrrFloor = 0.80
	if slugMRR < mrrFloor {
		t.Errorf("slug 정답질의 MRR %.3f < %.2f — 점수식이 정확한 회수를 깨뜨렸다", slugMRR, mrrFloor)
	}
	if tailMRR < mrrFloor {
		t.Errorf("요약뒤쪽 정답질의 MRR %.3f < %.2f — 요약의 꼬리로 그 노트를 못 찾는다",
			tailMRR, mrrFloor)
	}

	// ── 실제 프롬프트로 재는 길이 편향 ───────────────────────────────
	prompts := loadPrompts(t)
	if len(prompts) == 0 {
		t.Log("PRIORCASE_MEASURE_PROMPTS 가 없다 — 길이 편향은 건너뛴다 (합성 질의로는 재현되지 않는다)")
		return
	}
	injected := map[string]int{}
	total, withHit := 0, 0
	for _, p := range prompts {
		hits, _, err := Recall(l, c, p.Prompt, Options{
			// 훅과 같은 옵션이어야 한다. 여기서 갈리면 재는 것이 실제 주입이 아니다.
			Cwd: p.Cwd, CrossProject: true, Limit: 3, MinScore: 1,
			IncludeReferences: true, ReferenceLimit: 2, RuleLimit: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) > 0 {
			withHit++
		}
		for _, h := range hits {
			injected[h.Note.Path]++
			total++
		}
	}
	if total == 0 {
		t.Fatal("프롬프트 세트에서 주입이 한 건도 안 나왔다")
	}

	counts := make([]int, 0, len(injected))
	for _, v := range injected {
		counts = append(counts, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(counts)))
	top := func(k int) float64 {
		sum := 0
		for i := 0; i < k && i < len(counts); i++ {
			sum += counts[i]
		}
		return 100 * float64(sum) / float64(total)
	}
	t.Logf("프롬프트 %d개 · 주입 줄 %d · 서로 다른 문서 %d · 히트 있는 프롬프트 %d",
		len(prompts), total, len(injected), withHit)
	t.Logf("집중도: 상위5 %.1f%% · 상위10 %.1f%%", top(5), top(10))

	// ★ **핵심 지표: head 길이 4분위별 평균 주입 횟수.**
	//
	// 정규화 전 실측이 0.71 · 0.90 · 2.37 · 7.06 (Q4/Q1 = 10.0배)이었고,
	// ref=200/b=0.5 로 1.07 · 1.35 · 3.25 · 5.38 (5.0배)이 됐다.
	type pl struct {
		path string
		hl   int
	}
	all := make([]pl, 0, len(byPath))
	for p, hl := range byPath {
		all = append(all, pl{p, hl})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].hl != all[j].hl {
			return all[i].hl < all[j].hl
		}
		return all[i].path < all[j].path
	})
	q := len(all) / 4
	means := make([]float64, 4)
	for i := 0; i < 4; i++ {
		lo, hi := i*q, (i+1)*q
		if i == 3 {
			hi = len(all)
		}
		sum, zero := 0, 0
		for _, x := range all[lo:hi] {
			sum += injected[x.path]
			if injected[x.path] == 0 {
				zero++
			}
		}
		means[i] = float64(sum) / float64(hi-lo)
		t.Logf("  Q%d head %4d~%4d자 (n=%d): 평균 %5.2f회 · 0회 %d건(%.0f%%)",
			i+1, all[lo].hl, all[hi-1].hl, hi-lo, means[i], zero,
			100*float64(zero)/float64(hi-lo))
	}
	if means[0] > 0 {
		t.Logf("⇒ Q4/Q1 길이 편향: %.1f배 (정규화 없을 때 실측 10.0배)", means[3]/means[0])
	}
	dumpIfAsked(t, l, injected)
}

// headOf 는 scoreAll 이 보는 head 를 그대로 만든다. 여기서 갈리면 재는 것이 다른 것이다.
// **그래서 직접 조립하지 않고 headText 를 부른다** — 예전에는 여기서 조립했고,
// 그 사본이 stem 의 날짜·도메인 접두어를 그대로 남긴 채 편향을 재고 있었다.
func headOf(l *store.Layout, n store.Note) string {
	return headText(n, l.DecisionMarker())
}

func pct(sorted []int, p float64) int {
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

// knownAnswers 는 정답을 아는 질의 세트를 돌려 MRR 을 준다.
func knownAnswers(t *testing.T, l *store.Layout, c *config.Config,
	notes []store.Note, label string, mk func(store.Note) string) float64 {
	t.Helper()
	var n, miss, at1, at3 int
	var mrr float64
	for _, note := range notes {
		q := mk(note)
		if q == "" {
			continue
		}
		n++
		rank := 0
		hits, _, err := Recall(l, c, q, Options{CrossProject: true, Limit: 20, MinScore: 1})
		if err != nil {
			t.Fatal(err)
		}
		for i, h := range hits {
			if h.Note.Path == note.Path {
				rank = i + 1
				break
			}
		}
		switch {
		case rank == 0:
			miss++
		default:
			mrr += 1 / float64(rank)
			if rank == 1 {
				at1++
			}
			if rank <= 3 {
				at3++
			}
		}
	}
	if n == 0 {
		return 1
	}
	mrr /= float64(n)
	t.Logf("정답질의(%s) n=%d: 못찾음 %d · @1 %d(%.0f%%) · @3 %d(%.0f%%) · MRR %.3f",
		label, n, miss, at1, 100*float64(at1)/float64(n), at3, 100*float64(at3)/float64(n), mrr)
	return mrr
}

// slugQuery 는 `{domain}-결정-{slug}-{date}` 에서 slug 토큰 3개를 뽑는다.
//
// **도메인 접두어(첫 토큰)는 뺀다.** weightMention(+6)이 가장 센 가중치라, 남기면
// 그 도메인 전체가 가점을 받아 측정이 도메인 이름 하나에 지배된다.
func slugQuery(n store.Note) string {
	parts := strings.Split(n.Stem, "-")
	var toks []string
	for _, p := range parts {
		if p == "" || p == "결정" || p == "decision" || isAllDigits(p) || len([]rune(p)) < 3 {
			continue
		}
		toks = append(toks, p)
	}
	if len(toks) <= 1 {
		return ""
	}
	toks = toks[1:]
	if len(toks) > 3 {
		toks = toks[:3]
	}
	return strings.Join(toks, " ")
}

// tailQuery 는 요약의 마지막 1/3 에서 긴 낱말 3개를 뽑는다.
func tailQuery(summary string) string {
	r := []rune(summary)
	if len(r) < 60 {
		return "" // 짧은 요약에는 "뒤쪽" 이 없다
	}
	fields := ExtractKeywords(string(r[len(r)*2/3:]))
	sort.Slice(fields, func(i, j int) bool {
		if len(fields[i]) != len(fields[j]) {
			return len(fields[i]) > len(fields[j])
		}
		return fields[i] < fields[j]
	})
	var toks []string
	for _, f := range fields {
		if len([]rune(f)) < 3 {
			continue
		}
		toks = append(toks, f)
		if len(toks) == 3 {
			break
		}
	}
	if len(toks) < 2 {
		return ""
	}
	return strings.Join(toks, " ")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type measurePrompt struct {
	Cwd    string `json:"cwd"`
	Prompt string `json:"prompt"`
}

func loadPrompts(t *testing.T) []measurePrompt {
	t.Helper()
	p := os.Getenv("PRIORCASE_MEASURE_PROMPTS")
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("프롬프트 파일을 못 읽는다: %v", err)
	}
	var out []measurePrompt
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("프롬프트 파일이 JSON 배열이 아니다: %v", err)
	}
	return out
}

// dumpIfAsked 는 노트별 주입 횟수를 TSV 로 흘린다 (사전/사후 대조용).
func dumpIfAsked(t *testing.T, l *store.Layout, injected map[string]int) {
	t.Helper()
	out := os.Getenv("PRIORCASE_MEASURE_OUT")
	if out == "" {
		return
	}
	type row struct {
		path string
		n    int
	}
	rows := make([]row, 0, len(injected))
	for p, n := range injected {
		rows = append(rows, row{p, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].path < rows[j].path
	})
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%d\t%s\n", r.n, l.RelPath(r.path))
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("측정 결과를 못 쓴다: %v", err)
	}
	t.Logf("노트별 주입 횟수를 %s 에 썼다", out)
}

// realVaultConfig 는 **폴더 구조에서** 설정을 유도한다 (이 파일 머리말의 § 참고).
//
// `paths` 는 비운다 — 이 머신의 프로젝트 경로를 알 수 없고, 알 필요도 없다.
// cwd 는 프롬프트 세트가 직접 준다.
func realVaultConfig(t *testing.T, vault string) *config.Config {
	t.Helper()
	entries, err := os.ReadDir(vault)
	if err != nil {
		t.Fatalf("볼트를 읽을 수 없다: %v", err)
	}
	var domains []config.Domain
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(vault, e.Name(), "decisions")); err != nil {
			continue
		}
		domains = append(domains, config.Domain{Prefix: e.Name(), Folder: e.Name()})
	}
	if len(domains) == 0 {
		t.Fatalf("`<도메인>/decisions/` 폴더를 하나도 못 찾았다 (%s)", vault)
	}
	return &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "common",
		Lang:          "ko",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
		},
		Domain: domains,
	}
}
