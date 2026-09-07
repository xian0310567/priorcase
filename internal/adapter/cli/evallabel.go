package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// ── 실물 라벨을 볼트와 트랜스크립트에서 만든다 ────────────────────────
//
// ★★★ **이 파일이 있는 이유는 대리지표가 낙관적이기 때문이다.**
//
// 링크쌍 프록시는 볼트에서 공짜로 나오지만 질의가 **요약 문장**이다 — 볼트 어휘로
// 촘촘히 쓰인 완성된 문장이라 어휘 랭커에 유리하다. 같은 날 같은 코드로 잰 수가
// 이만큼 갈렸다: 링크쌍 어휘미스 29.8% 대 실물 라벨 76.9%. **2.6배**다
// (`규칙-대리지표는-실물로-다시-잰다`).
//
// 그런데 실물 라벨은 사람이 손으로 붙여야 해서 지난번에 n=26 에서 멈췄고,
// *"76.9% 는 방향은 믿을 만하나 소수점은 못 믿는다"* 로 끝났다.
//
// # 손을 안 대고 실물 라벨을 만드는 법
//
// 결정 노트는 **자기가 어느 대화에서 나왔는지**(`source_session`)와 **그때 근거로
// 삼은 결정이 무엇인지**(`related`)를 이미 들고 있다. 둘을 이으면 라벨이 된다:
//
//	그 세션의 그 지점에서 사람이 친 말  →  그때 실제로 필요했던 과거 결정
//
// 이건 추측이 아니라 **기록된 사실**이다. 노트를 쓴 에이전트가 그 시점에 그
// 결정을 근거로 골랐다. 회수가 해야 했던 일이 정확히 그것을 그 자리에 꺼내
// 놓는 것이다.
//
// # 알고 있는 한계
//
//   - `related` 는 회수가 꺼내 줘서 걸린 것일 수도 있고, 에이전트가 볼트를 직접
//     뒤져서 찾은 것일 수도 있다. 후자여도 **"그 자리에서 필요했던 노트"** 라는
//     라벨의 뜻은 그대로다 — 회수가 못 꺼내서 손으로 찾은 것이니 오히려 실패 사례다.
//   - 앵커(그 대화의 어느 지점에서 노트가 쓰였나)를 슬러그·요약 앞머리로 찾는다.
//     못 찾으면 그 노트는 **버린다.** 세션의 마지막 발화로 대충 때우면 라벨이
//     조용히 틀리고, 틀린 라벨로 잰 수는 없는 것만 못하다.
//   - 프롬프트에 정답 stem 이 그대로 박혀 있으면 뺀다. 그건 회수가 아니라 받아쓰기다.
//   - 정답이 질의 세션보다 **나중에 생긴** 노트면 뺀다. 회수가 꺼낼 수 없었던 것이다.

// labelOpts 는 라벨 만들기 옵션이다.
type labelOpts struct {
	// ContextRunes 는 프롬프트 앞의 대화를 몇 글자까지 담을지다.
	//
	// 4,000 은 2026-09-02 실측이 쓴 값이다(MRR 0.022→0.203). **라벨에는 넉넉히
	// 담고 자르는 것은 재는 쪽에서 한다** — 담지 않은 것은 나중에 늘릴 수 없지만
	// 담아 둔 것은 언제든 줄일 수 있다.
	ContextRunes int
	// MinPromptRunes 는 프롬프트로 인정할 최소 길이다. 훅의 minPromptLen 과 같다 —
	// 훅이 회수하지 않는 프롬프트를 라벨에 넣으면 재는 것이 실제 경로가 아니다.
	MinPromptRunes int
}

// buildLabels 는 볼트와 트랜스크립트에서 실물 라벨을 만든다.
// buildLabels 는 볼트와 트랜스크립트에서 실물 라벨을 만든다.
//
// rules·refs 를 같이 받는 이유는 **정답이 결정만이 아니기 때문**이다:
//
//   - 결정 → 그 결정이 `related` 로 건 **결정**   (원래 하던 것)
//   - 결정 → 그 결정이 `related` 로 건 **참고**   (근거 문서도 그 자리에 필요했다)
//   - 결정 → 그 결정을 출처로 삼는 **규칙**       (방향이 거꾸로다, 아래 §)
//
// # 규칙만 방향이 거꾸로인 이유
//
// 결정이 규칙을 가리키는 링크는 이 볼트에 **2개뿐**이다(결정 685건 중). 사람은
// 규칙을 근거로 인용하지 않는다 — 지키는 것이라서다. 반대로 12개 규칙 **전부**가
// 출처 결정을 26개 걸고 있다. 그래서 규칙 라벨은 이렇게 읽는다:
// *"이 결정이 내려진 그 대화에서, 이 결정에서 증류된 규칙이 걸렸어야 한다."*
func buildLabels(errw io.Writer, c *config.Config, l *store.Layout, notes, rules, refs []store.Note, o labelOpts) ([]labeled, error) {
	if o.ContextRunes <= 0 {
		o.ContextRunes = 4000
	}
	if o.MinPromptRunes <= 0 {
		o.MinPromptRunes = 8
	}
	index, err := sessionIndex(c)
	if err != nil {
		return nil, err
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("트랜스크립트를 하나도 못 찾았다 — 호스트 기록 자리를 보라")
	}

	byStem := map[string]store.Note{}
	for _, n := range notes {
		byStem[n.Stem] = n
	}
	refStem := map[string]bool{}
	for _, n := range refs {
		refStem[n.Stem] = true
	}
	// 규칙은 역방향 색인이 필요하다 (이 함수의 §): 결정 stem → 그것을 출처로 삼는 규칙들.
	ruleOf := map[string][]string{}
	for _, r := range rules {
		for _, src := range r.Links() {
			ruleOf[src] = append(ruleOf[src], r.Stem)
		}
	}

	// 세션 하나를 여러 노트가 공유한다. 파일을 노트마다 다시 파싱하면 같은
	// 트랜스크립트를 몇 번씩 읽는다 — 큰 것은 51MB 다.
	bySession := map[string][]store.Note{}
	for _, n := range notes {
		s := strings.TrimSpace(n.Meta.SourceSession)
		// **related 가 없어도 규칙 라벨은 나올 수 있다** — 규칙이 이 결정을
		// 가리키는 것이지 이 결정이 규칙을 가리키는 것이 아니다.
		if s == "" || (len(n.Meta.Related) == 0 && len(ruleOf[n.Stem]) == 0) {
			continue
		}
		bySession[s] = append(bySession[s], n)
	}

	var (
		out                                       []labeled
		noTranscript, noAnchor, noPrompt, dropped int
	)
	sessions := make([]string, 0, len(bySession))
	for s := range bySession {
		sessions = append(sessions, s)
	}
	sort.Strings(sessions)

	for _, sid := range sessions {
		hit, ok := index[sid]
		if !ok {
			noTranscript += len(bySession[sid])
			continue
		}
		turns, meta, perr := parseTranscript(hit)
		if perr != nil || len(turns) == 0 {
			noTranscript += len(bySession[sid])
			continue
		}
		for _, n := range bySession[sid] {
			at := anchorTurn(turns, n)
			if at < 0 {
				noAnchor++
				continue
			}
			prompt, ctx, ctxUser := promptBefore(turns, at, o)
			if prompt == "" {
				noPrompt++
				continue
			}
			targets := labelTargets(n, byStem, refStem, ruleOf, prompt)
			if len(targets) == 0 {
				dropped++
				continue
			}
			cwd := meta.Cwd
			for _, tgt := range targets {
				out = append(out, labeled{
					Prompt:      prompt,
					Cwd:         cwd,
					Want:        tgt.stem,
					Kind:        tgt.kind,
					Context:     ctx,
					ContextUser: ctxUser,
					ID:          "label:" + tgt.kind + ":" + n.Stem + "→" + tgt.stem,
				})
			}
		}
	}
	fmt.Fprintf(errw,
		"라벨 %d건 · 버린 것: 트랜스크립트 없음 %d · 앵커 못 찾음 %d · 프롬프트 없음 %d · 정답 없음 %d\n",
		len(out), noTranscript, noAnchor, noPrompt, dropped)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// sessionIndex 는 세션 id → 기록 파일이다.
//
// **파일 이름으로 찾는다.** 전건을 파싱해 meta.SessionID 를 읽으면 정확하지만
// 이 기계에서만 1,492개 파일 369MB 다. Claude Code 는 파일명이 곧 세션 id 이고
// Codex 는 파일명 끝에 uuid 가 붙는다 — 둘 다 이름으로 갈린다.
//
// **호스트는 daemon.ResolveHosts 로 푼다.** 설정에서 끈 호스트를 여기서만 계속
// 읽으면 라벨이 그 호스트 대화로 만들어지고, 사람은 껐다고 믿는다 (그 통로가
// 하나여야 하는 이유는 daemon/hostsel.go 의 §).
func sessionIndex(c *config.Config) (map[string]transcriptHit, error) {
	rs, err := daemon.ResolveHosts(c, "")
	if err != nil {
		return nil, err
	}
	idx := map[string]transcriptHit{}
	for _, r := range rs {
		paths, _, lerr := r.Host.List(r.Root)
		if lerr != nil {
			continue
		}
		for _, p := range paths {
			stem := strings.TrimSuffix(filepath.Base(p), ".jsonl")
			h := transcriptHit{path: p, host: r}
			idx[stem] = h
			// Codex: `rollout-<타임스탬프>-<uuid>`. 끝의 uuid 로도 걸리게 둔다.
			if i := strings.LastIndex(stem, "-"); i > 0 && len(stem)-i-1 == 12 {
				if j := lastUUID(stem); j != "" {
					idx[j] = h
				}
			}
		}
	}
	return idx, nil
}

type transcriptHit struct {
	path string
	host hosts.Resolved
}

// lastUUID 는 이름 끝의 uuid 를 뽑는다. 못 뽑으면 빈 값이다.
func lastUUID(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) < 5 {
		return ""
	}
	u := strings.Join(parts[len(parts)-5:], "-")
	if len(u) != 36 {
		return ""
	}
	return u
}

func parseTranscript(h transcriptHit) ([]transcript.Turn, transcript.Meta, error) {
	f, err := os.Open(h.path)
	if err != nil {
		return nil, transcript.Meta{}, err
	}
	defer f.Close()
	turns, meta, _, _, perr := h.host.Host.Parse(f)
	return turns, meta, perr
}

// anchorTurn 은 이 노트가 **쓰인 지점**을 찾는다. 못 찾으면 -1 이다.
//
// 슬러그 몸통(도메인·`결정`·날짜를 뺀 가운데)이나 요약 앞머리가 발화에 나오면
// 거기가 capture 호출이다. **마지막 등장을 쓴다** — 같은 주제를 여러 번 말하다가
// 마지막에 기록하는 것이 보통이고, 첫 등장을 쓰면 기록 이전의 탐색 구간을 앵커로
// 삼게 된다.
func anchorTurn(turns []transcript.Turn, n store.Note) int {
	needles := make([]string, 0, 2)
	if core := slugCore(n.Stem); len([]rune(core)) >= 6 {
		needles = append(needles, core)
	}
	if s := strings.TrimSpace(n.Meta.Summary); len([]rune(s)) >= 20 {
		needles = append(needles, string([]rune(s)[:20]))
	}
	if len(needles) == 0 {
		return -1
	}
	for i := len(turns) - 1; i >= 0; i-- {
		for _, nd := range needles {
			if strings.Contains(turns[i].Text, nd) {
				return i
			}
		}
	}
	return -1
}

// slugCore 는 `{domain}-결정-{slug}-{date}` 에서 slug 만 남긴다.
func slugCore(stem string) string {
	parts := strings.Split(stem, "-")
	if len(parts) < 6 {
		return ""
	}
	// 앞 둘(도메인·표식)과 뒤 셋(YYYY-MM-DD)을 뗀다.
	return strings.Join(parts[2:len(parts)-3], "-")
}

// promptBefore 는 앵커 **앞의** 마지막 사람 발화와 그보다 앞선 대화를 준다.
//
// **앵커 자신은 포함하지 않는다.** 앵커는 기록하는 순간이고, 회수가 도와야 했던
// 것은 그 앞의 대화다.
func promptBefore(turns []transcript.Turn, at int, o labelOpts) (prompt, ctx, ctxUser string) {
	pi := -1
	for i := at - 1; i >= 0; i-- {
		if turns[i].Kind != transcript.KindUser || turns[i].Sidechain {
			continue
		}
		t := strings.TrimSpace(turns[i].Text)
		if len([]rune(t)) < o.MinPromptRunes {
			continue
		}
		pi, prompt = i, t
		break
	}
	if pi < 0 {
		return "", "", ""
	}
	// 맥락은 그 발화 **앞**의 대화를 뒤에서부터 채운다. 직전 화제가 주제라서다.
	return prompt,
		gather(turns[:pi], o.ContextRunes, func(transcript.Turn) bool { return true }),
		gather(turns[:pi], o.ContextRunes, func(t transcript.Turn) bool {
			return t.Kind == transcript.KindUser && !t.Sidechain
		})
}

// gather 는 발화를 **뒤에서부터** 골라 담고 원래 순서로 되돌린다.
func gather(turns []transcript.Turn, limit int, keep func(transcript.Turn) bool) string {
	var parts []string
	total := 0
	for i := len(turns) - 1; i >= 0 && total < limit; i-- {
		if !keep(turns[i]) {
			continue
		}
		t := strings.TrimSpace(turns[i].Text)
		if t == "" {
			continue
		}
		r := []rune(t)
		if total+len(r) > limit {
			r = r[len(r)-(limit-total):]
		}
		parts = append(parts, string(r))
		total += len(r)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n")
}

// target 은 정답 하나와 그 계층이다.
type target struct{ stem, kind string }

// labelTargets 는 이 노트에서 **정답으로 쓸 수 있는 것**만 남긴다 (buildLabels 의 §).
func labelTargets(n store.Note, byStem map[string]store.Note, refStem map[string]bool,
	ruleOf map[string][]string, prompt string) []target {
	var out []target
	add := func(stem, kind string) {
		// 프롬프트에 stem 이 그대로 박혀 있으면 회수가 아니라 받아쓰기다.
		if stem == "" || stem == n.Stem || strings.Contains(prompt, stem) {
			return
		}
		out = append(out, target{stem, kind})
	}
	for _, raw := range n.Meta.Related {
		tgt := store.NFC(strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "[]")))
		switch t, ok := byStem[tgt]; {
		case ok:
			// **나중에 생긴 노트는 못 꺼낸다.** 날짜로 거른다. 같은 날은 남긴다 —
			// 하루 안의 순서는 날짜로 알 수 없고, 같은 날 결정을 근거로 삼는 것은 흔하다.
			if t.Meta.Date > n.Meta.Date {
				continue
			}
			add(tgt, "decision")
		case refStem[tgt]:
			// 참고에는 날짜 규약이 없는 것이 많아 시점 검사를 안 건다.
			add(tgt, "reference")
		}
	}
	// **규칙은 역방향이다** — 이 결정을 출처로 삼는 규칙이 정답이다.
	//
	// 규칙이 그 결정보다 나중에 생기는 것은 **정상**이다(증류는 사후에 한다).
	// 그래서 여기에는 날짜 검사를 걸지 않는다 — 걸면 라벨이 전부 사라진다.
	// 대신 그 대가를 안다: 이 라벨은 "그때 회수가 꺼낼 수 있었나" 가 아니라
	// **"지금 같은 대화가 다시 오면 걸리나"** 를 재는 것이다.
	for _, r := range ruleOf[n.Stem] {
		add(r, "rule")
	}
	return out
}

// writeLabels 는 라벨 파일을 쓴다.
func writeLabels(path string, ls []labeled) error {
	b, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
