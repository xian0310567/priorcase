package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// ★★★ **임계 미만으로 끝난 파일이 시딩되면 그 대화가 통째로 사라진다.**
//
// Scan 은 임계 미만이면 전진하지 않는다 (발화를 누적하려는 설계다). 그러면
// Offset 이 0 으로 굳는데, PlanSweep 은 **Offset 으로 "아는 파일" 을 판정한다** —
// 그래서 이미 훑은 파일이 "처음 보는 파일" 로 분류돼 끝으로 시딩된다.
//
// 시딩은 offset 을 파일 끝으로 밀므로, 그 앞의 발화는 **영영 안 읽힌다.**
// 세션이 이어져서 나중에 임계를 넘겨도 앞부분은 이미 건너뛴 뒤다.
//
// 이건 안전망 자신의 데이터 손실이다 — 놓친 결정을 줍는 장치가 대화를 버린다.
func TestBelowThresholdFileIsNotSeededAway(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)

	// 임계(6) 미만인 짧은 세션.
	p := filepath.Join(root, "proj", "s1.jsonl")
	writeTurns(t, p, 3, "가")

	if _, err := Scan(st, c, l, p, false, rs); err != nil {
		t.Fatalf("첫 스캔: %v", err)
	}

	// 데몬이 다시 뜨면서 훑기를 계획한다.
	plan, err := PlanSweep(st, rs, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Seed {
		if s == p {
			t.Fatalf("이미 훑은 파일이 시딩 대상이 됐다 — 앞선 발화 3개가 사라진다\n"+
				"  체크포인트: %+v", st.CheckpointSnapshot()[p])
		}
	}
}

// ★★★ **누적이 실제로 이어져야 한다.**
//
// 짧은 세션이 이어져 임계를 넘기면 그 구간이 표시돼야 한다. 앞부분을 잃으면
// 두 번째 조각만 세어 영원히 임계 미만으로 남는다.
func TestTurnsAccumulateAcrossScans(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)
	p := filepath.Join(root, "proj", "s2.jsonl")

	// 3발화 → 임계(6) 미만
	writeTurns(t, p, 3, "결정")
	r1, err := Scan(st, c, l, p, false, rs)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Flagged {
		t.Fatal("3발화인데 표시됐다")
	}

	// 대화가 이어져 3발화가 더 붙는다 → 누적 6 = 임계
	appendTurns(t, p, 3, "선택")
	r2, err := Scan(st, c, l, p, false, rs)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Flagged {
		t.Fatalf("누적 6발화인데 표시되지 않았다 (이번 구간 %d발화) — 앞 조각을 잃었다", r2.Turns)
	}
	// 표시된 구간이 **처음부터** 덮어야 한다. 뒤 조각만 담으면 사람이 앞부분을
	// 못 보고 판단한다.
	ps := st.Pending()
	if len(ps) != 1 {
		t.Fatalf("pending %d건 — 1건이어야 한다", len(ps))
	}
	if ps[0].From != 0 {
		t.Errorf("표시된 구간이 %d 바이트부터다 — 0 부터여야 앞 조각이 들어간다", ps[0].From)
	}
	if !strings.Contains(ps[0].Excerpt, "결정") {
		t.Errorf("발췌에 앞 조각이 없다:\n%s", ps[0].Excerpt)
	}
}

// ★★ 임계를 넘긴 뒤에는 누적이 0 으로 돌아가야 한다. 안 그러면 그 다음 한 발화가
// 곧바로 임계를 넘겨 매번 표시된다.
func TestAccumulationResetsAfterFlag(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)
	p := filepath.Join(root, "proj", "s3.jsonl")

	writeTurns(t, p, 6, "결정")
	if r, err := Scan(st, c, l, p, false, rs); err != nil || !r.Flagged {
		t.Fatalf("6발화가 표시돼야 한다 (err=%v flagged=%v)", err, r.Flagged)
	}
	appendTurns(t, p, 1, "선택")
	r, err := Scan(st, c, l, p, false, rs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Flagged {
		t.Error("1발화가 곧바로 표시됐다 — 누적이 초기화되지 않았다")
	}
}

// ★★ **파일이 잘리면 누적도 버려야 한다.**
//
// 세션 파일이 지난번보다 작아졌으면 잘렸거나 다른 파일로 바뀐 것이라
// CheckpointFor 가 0 을 준다. 그때 앞 파일의 누적 발화 수를 그대로 들고 가면
// **없어진 대화의 발화를 세어** 임계를 넘긴다 — 사람에게 존재하지 않는 구간을
// 보여 주게 된다. Seg 도 새 파일의 엉뚱한 자리를 가리킨다.
func TestTruncationDropsAccumulation(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)
	p := filepath.Join(root, "proj", "s4.jsonl")

	// 5발화 — 임계(6) 미만으로 누적된다.
	writeTurns(t, p, 5, "결정")
	if _, err := Scan(st, c, l, p, false, rs); err != nil {
		t.Fatal(err)
	}
	if cp := st.CheckpointEntry(p); cp.Turns != 5 {
		t.Fatalf("준비: 누적 %d턴 — 5턴이어야 한다", cp.Turns)
	}

	// 파일이 다른 세션으로 갈렸다 (더 작다).
	writeTurns(t, p, 2, "선택")

	r, err := Scan(st, c, l, p, false, rs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Turns != 2 {
		t.Errorf("발화 %d개로 셌다 — 2개여야 한다. 없어진 파일의 누적을 들고 왔다", r.Turns)
	}
	if r.Flagged {
		t.Error("2발화인데 표시됐다 — 잘린 앞 파일의 누적이 임계를 밀어 올렸다")
	}
}

// ★ 표시한 뒤에는 상태 파일에 **죽은 구간 시작점이 남지 않아야** 한다.
//
// 지금 흐름에서는 다시 안 읽히지만, 사람이 state.json 을 열어 보는데 그건
// 데이터처럼 보인다 (At 에 omitzero 를 쓴 것과 같은 이유다).
func TestFlagClearsSegmentStartInState(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)
	p := filepath.Join(root, "proj", "s5.jsonl")

	writeTurns(t, p, 3, "결정")
	if _, err := Scan(st, c, l, p, false, rs); err != nil {
		t.Fatal(err)
	}
	if cp := st.CheckpointEntry(p); cp.Seg != 0 {
		t.Fatalf("준비: 첫 구간의 Seg 는 0 이어야 한다 (%d)", cp.Seg)
	}
	appendTurns(t, p, 4, "선택") // 누적 7 → 표시
	if r, err := Scan(st, c, l, p, false, rs); err != nil || !r.Flagged {
		t.Fatalf("표시돼야 한다 (err=%v flagged=%v)", err, r.Flagged)
	}
	cp := st.CheckpointEntry(p)
	if cp.Turns != 0 || cp.Seg != 0 {
		t.Errorf("표시 뒤에 누적이 남았다: Turns=%d Seg=%d", cp.Turns, cp.Seg)
	}
	// 상태 파일 자체에도 안 남아야 한다.
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"seg"`) {
		t.Errorf("state.json 에 죽은 seg 가 남았다:\n%s", b)
	}
}

// ★★ **AdvanceAccum 의 계약: turns 가 0 이면 누적이 끝났다는 뜻이다.**
//
// 그때는 넘어온 seg 를 무시하고 지운다. 지금 부르는 쪽은 늘 (0, 0) 을 넘기지만,
// 그 사실에 기대면 호출부가 하나 늘 때 조용히 죽은 시작점이 남는다 — 그러면
// 다음 구간이 옛 자리부터 다시 읽힌다.
func TestAdvanceAccumClearsSegWhenTurnsZero(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	const p = "/t.jsonl"
	if err := st.AdvanceAccum(p, 100, 100, 3, 40); err != nil {
		t.Fatal(err)
	}
	if cp := st.CheckpointEntry(p); cp.Turns != 3 || cp.Seg != 40 {
		t.Fatalf("누적 중: Turns=%d Seg=%d — 3/40 이어야 한다", cp.Turns, cp.Seg)
	}
	// turns=0 인데 seg 를 0 이 아닌 값으로 넘긴다.
	if err := st.AdvanceAccum(p, 200, 200, 0, 40); err != nil {
		t.Fatal(err)
	}
	cp := st.CheckpointEntry(p)
	if cp.Turns != 0 {
		t.Errorf("Turns=%d — 0 이어야 한다", cp.Turns)
	}
	if cp.Seg != 0 {
		t.Errorf("Seg=%d — 누적이 끝났으면 0 이어야 한다", cp.Seg)
	}
}

// ── 픽스처 ────────────────────────────────────────────────────────────────

func accCfg(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "proj", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "proj",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain:  []config.Domain{{Prefix: "proj", Folder: "proj"}},
		Capture: config.Capture{MinTurns: 6, Signals: []string{"결정", "선택"}},
	}
	return c, store.NewLayout(c)
}

func accHosts(t *testing.T, root string) []hosts.Resolved {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Claude Code 호스트를 이 임시 루트로 묶는다.
	h := hosts.All()[0]
	return []hosts.Resolved{{Host: h, Root: root}}
}

func writeTurns(t *testing.T, path string, n int, word string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(turnLines(t, 0, n, word)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTurns(t *testing.T, path string, n int, word string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(turnLines(t, 100, n, word)); err != nil {
		t.Fatal(err)
	}
}

// turnLines 는 Claude Code 기록 모양의 줄을 만든다.
func turnLines(t *testing.T, base, n int, word string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		rec := map[string]any{
			"type":      role,
			"sessionId": "S1",
			"cwd":       "/tmp/proj",
			"timestamp": fmt.Sprintf("2026-08-13T%02d:00:00Z", (base+i)%24),
			"message": map[string]any{
				"role":    role,
				"content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("%s %d번째 발화다", word, base+i)}},
			},
		}
		j, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return b.String()
}
