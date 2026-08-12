package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/testutil"

	"github.com/xian0310567/priorcase/internal/core/health"
	"github.com/xian0310567/priorcase/internal/core/retro"
)

// ★★ **이건 앱과의 계약이다. 조용히 바뀌면 앱이 깨진다.**
//
// `prior queue --json` 은 데스크탑 앱이 읽는 유일한 표면이다. 사람이 읽는 출력은
// 문구를 자유롭게 고쳐도 되지만 여기는 다르다 — 키 하나를 이름만 바꿔도 앱의
// 화면이 통째로 빈다. 그리고 **Go 구조체를 고치면 컴파일은 그대로 통과한다.**
//
// 그래서 키 목록을 여기 박아 둔다. 바꾸려면 이 테스트도 같이 고쳐야 하고, 그
// 순간이 "앱도 같이 고쳐야 한다" 를 알아채는 자리다.
func TestQueueJSONContract(t *testing.T) {
	keys := func(v any) []string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		var out []string
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	want := func(name string, v any, exp ...string) {
		t.Helper()
		got := keys(v)
		set := map[string]bool{}
		for _, k := range got {
			set[k] = true
		}
		for _, e := range exp {
			if !set[e] {
				t.Errorf("%s 에 %q 키가 없다 — 앱이 그 자리를 못 읽는다 (지금: %v)", name, e, got)
			}
		}
		if len(got) != len(exp) {
			t.Errorf("%s 의 키가 %v 인데 계약은 %v 다 — 늘었으면 앱에도 알려야 하고, "+
				"줄었으면 앱이 깨진다", name, got, exp)
		}
	}

	// 최상위. **빈 배열이어도 키는 있어야 한다** — 없으면 앱이 "아직 안 왔다" 와
	// "할 일이 없다" 를 구별하지 못한다.
	want("Queue", Queue{}, "confirm", "review", "retro", "health")
	// fails·gave_up 은 omitempty 가 아니다. 0/false 도 사실이고, 키가 빠지면 앱이
	// "실패한 적 없다" 와 "이 필드를 모르는 옛 버전이다" 를 구별하지 못한다.
	want("QueuePending", QueuePending{}, "id", "domain", "when", "signals", "excerpt",
		"fails", "gave_up", "similar")
	want("QueueSimilar", QueueSimilar{}, "stem", "path", "summary", "score")
	// **excerpt 는 omitempty 가 아니다.** 옛 원장 줄에는 없는데, 키까지 빠지면
	// 앱이 "발췌가 없다" 와 "필드를 모른다" 를 구별하지 못한다.
	want("QueueReview", QueueReview{}, "id", "domain", "at", "path", "excerpt")
	want("QueueCheck", QueueCheck{Fix: "x"}, "name", "level", "detail", "fix")
	want("retro.Item", retro.Item{Author: "x"},
		"stem", "date", "domain", "summary", "author", "reason", "hits")
}

// ★ **빈 큐는 `[]` 여야 한다. `null` 이면 안 된다.**
//
// Go 의 nil 슬라이스는 `null` 로 나간다. 앱 쪽에서 `null.length` 는 터지고,
// 그 터짐이 "할 일이 없다" 인지 "명령이 실패했다" 인지 구별되지 않는다.
func TestQueueEmptyIsArrayNotNull(t *testing.T) {
	q := Queue{Confirm: []QueuePending{}, Review: []QueueReview{},
		Retro: []retro.Item{}, Health: []QueueCheck{}}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"confirm":[]`, `"review":[]`, `"retro":[]`, `"health":[]`} {
		if !strings.Contains(s, k) {
			t.Errorf("빈 큐가 %s 로 안 나온다: %s", k, s)
		}
	}
	// warnings 는 반대다 — 없으면 아예 빠지는 것이 맞다. 있으면 큐가 불완전하다는
	// 뜻이므로, 빈 배열이 늘 붙어 있으면 앱이 그 신호를 못 알아본다.
	if strings.Contains(s, `"warnings"`) {
		t.Errorf("경고가 없는데 warnings 키가 나왔다: %s", s)
	}
}

// ★★ **중첩된 배열도 null 이면 안 된다.**
//
// 최상위 큐만 []로 맞추고 그 안의 배열을 놓쳤다. `signals` 가 그렇다 —
// **판별기가 있으면 시그널 필터를 건너뛰므로(D9) 비는 것이 정상이고 흔하다.**
// 예외가 아니라 일상이다.
//
// 실측으로 잡았다. 큐 27건 중 2건이 `"signals": null` 이었고 jq 가
// "Cannot iterate over null" 로 터졌다. 앱도 같은 자리에서 터진다.
//
// 키 존재만 보는 계약 테스트로는 이걸 못 잡는다 — 값의 모양까지 봐야 한다.
func TestQueueNestedArraysAreNeverNull(t *testing.T) {
	q := Queue{
		Confirm: []QueuePending{{ID: "x", Signals: []string{}, Similar: []QueueSimilar{}}},
		Review:  []QueueReview{},
		Retro:   []retro.Item{},
		Health:  []QueueCheck{},
	}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `:null`) {
		t.Errorf("어딘가 null 이 나갔다 — 앱이 순회하다 터진다: %s", b)
	}
	if !strings.Contains(string(b), `"signals":[]`) {
		t.Errorf("빈 signals 가 [] 로 안 나온다: %s", b)
	}

	// 슬라이스 필드를 nil 로 두면 반드시 null 이 된다. 계약 구조체의 모든 슬라이스가
	// 그 위험을 지니므로, 새 필드가 늘면 여기서 걸리게 한다.
	var nilSig Queue
	nilSig.Confirm = []QueuePending{{ID: "x"}} // Signals·Similar 를 안 채움
	nilSig.Review = []QueueReview{}
	nilSig.Retro = []retro.Item{}
	nilSig.Health = []QueueCheck{}
	b2, _ := json.Marshal(nilSig)
	if !strings.Contains(string(b2), `"signals":null`) {
		t.Fatal("이 테스트의 전제가 깨졌다 — nil 슬라이스가 null 로 안 나간다")
	}
	// 위가 null 로 나간다는 것이 곧 **호출부가 반드시 채워야 한다**는 뜻이다.
	// queue.go 가 그렇게 하는지는 아래에서 본다.
	if !strings.Contains(readSource(t, "queue.go"), "sig = []string{}") {
		t.Error("queue.go 가 nil signals 를 [] 로 채우지 않는다")
	}
	// similarFor 도 같은 함정을 가진다 — 비슷한 것이 없는 경우가 흔하다.
	if !strings.Contains(readSource(t, "queue.go"), "out := []QueueSimilar{}") {
		t.Error("similarFor 가 빈 결과를 [] 로 만들지 않는다 — null 이 나간다")
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ★★ **등급은 숫자가 아니라 이름으로 나가야 한다.**
//
// health.Level 은 iota 정수다. 그대로 내보내면 앱이 `0 == 정상` 을 외워야 하고,
// 상수 순서가 바뀌는 순간 **조용히 뒤집힌다** — 고장이 초록불로 그려진다.
//
// 그리고 모르는 값은 "unknown" 이다. 정상으로 뭉개면 하필 알아야 할 상태가 안 보인다.
func TestQueueLevelIsSelfDescribing(t *testing.T) {
	for _, c := range []struct {
		in   health.Level
		want string
	}{
		{health.OK, "ok"},
		{health.Warn, "warn"},
		{health.Fail, "fail"},
		{health.Level(99), "unknown"},
	} {
		if got := levelName(c.in); got != c.want {
			t.Errorf("levelName(%d) = %q, want %q", c.in, got, c.want)
		}
	}
	// 정수가 새어 나가지 않는지 직렬화로 확인한다.
	b, _ := json.Marshal(QueueCheck{Level: levelName(health.Warn)})
	if !strings.Contains(string(b), `"level":"warn"`) {
		t.Errorf("등급이 문자열로 안 나간다: %s", b)
	}
}

// ★ **내부 구조체를 계약에 그대로 실으면 안 된다.**
//
// daemon.Pending·daemon.Promotion·health.Check 는 내부 상태 구조다. 그걸 그대로
// 내보내면 내부 리팩터링이 앱을 깨고, 진단용 필드(reason·err)까지 계약에 얹힌다.
// 실제로 review 와 health 가 그 상태였다 — health 는 json 태그가 없어 Go 필드명이
// 그대로 나갔고 Level 이 정수였다.
func TestQueueDoesNotLeakInternalStructs(t *testing.T) {
	q := reflect.TypeOf(Queue{})
	for i := 0; i < q.NumField(); i++ {
		f := q.Field(i)
		el := f.Type
		if el.Kind() == reflect.Slice {
			el = el.Elem()
		}
		if el.Kind() != reflect.Struct {
			continue
		}
		pkg := el.PkgPath()
		if strings.Contains(pkg, "/internal/daemon") || strings.Contains(pkg, "/internal/core/health") {
			t.Errorf("Queue.%s 가 %s 를 그대로 싣는다 — 계약 구조체로 감싸라",
				f.Name, el.String())
		}
	}
}

// ★★ **손으로 만든 값으로는 배선 누락을 못 잡는다.**
//
// 위 TestQueueNestedArraysAreNeverNull 은 Queue 를 직접 구성해 null 을 검사한다.
// 그건 구조체가 직렬화될 때의 모양만 본다 — **명령이 그 필드를 채우는지는 안 본다.**
//
// 실제로 그 구멍에 빠졌다. similarFor 를 만들고 호출부에 안 붙였는데, 계약 테스트는
// 키 존재만 보고 null 테스트는 손으로 채운 값을 보느라 둘 다 통과했다. 실기기에서
// `similar: null` 이 나와서야 드러났고, 앱은 거기서 터진다.
//
// 그래서 이 테스트는 **명령을 실제로 돌린다.**
func TestQueueCommandFillsEveryArray(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	// daemon.DefaultDir() 은 $XDG_STATE_HOME/priorcase 다. 그 자리에 심어야
	// 명령이 실제로 이 pending 을 본다 — 아니면 큐가 비어 루프가 안 돌고
	// **테스트가 공허하게 통과한다.**
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	sd := filepath.Join(stateHome, "priorcase")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(daemon.Pending{
		Path: "/t.jsonl", From: 1, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, At: time.Now().UTC(),
		Excerpt: "저장 엔진을 어느 것으로 할지 정했다. 임베디드 DB 로 간다.",
	}); err != nil {
		t.Fatal(err)
	}
	cmd := newQueueCmd()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(cmd)
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"queue", "--json", "--config", cfgPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("queue 실행: %v (stderr=%s)", err, errb.String())
	}

	raw := out.String()
	if strings.Contains(raw, ":null") || strings.Contains(raw, ": null") {
		t.Errorf("명령 출력에 null 이 있다 — 앱이 순회하다 터진다:\n%s", raw)
	}
	var q Queue
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatalf("출력이 JSON 이 아니다: %v\n%s", err, raw)
	}
	if q.Confirm == nil || q.Review == nil || q.Retro == nil || q.Health == nil {
		t.Fatal("최상위 배열 중 nil 이 있다")
	}
	// **큐가 비면 이 테스트는 아무것도 검사하지 않는다.** 배선 누락을 잡으려면
	// 루프가 실제로 돌아야 한다.
	if len(q.Confirm) == 0 {
		t.Fatalf("확인 큐가 비었다 — 명령이 상태 디렉토리를 못 찾았다 (경고: %v)", q.Warnings)
	}
	for _, c := range q.Confirm {
		if c.Signals == nil {
			t.Errorf("%s: signals 가 nil", c.ID)
		}
		if c.Similar == nil {
			t.Errorf("%s: similar 가 nil — similarFor 가 호출부에 안 붙었다", c.ID)
		}
	}
}
