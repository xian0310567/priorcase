package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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
	want("QueuePending", QueuePending{}, "id", "domain", "when", "signals", "excerpt")
	want("QueueReview", QueueReview{}, "id", "domain", "at", "path")
	want("QueueCheck", QueueCheck{Fix: "x"}, "name", "level", "detail", "fix")
	want("retro.Item", retro.Item{Author: "x"},
		"stem", "date", "domain", "summary", "author", "reason", "hits")
}

// ★ **빈 큐는 `[]` 여야 한다. `null` 이면 안 된다.**
//
// Go 의 nil 슬라이스는 `null` 로 나간다. 앱 쪽에서 `null.length` 는 터지고,
// 그 터짐이 "할 일이 없다" 인지 "명령이 실패했다" 인지 구별되지 않는다.
func TestQueueEmptyIsArrayNotNull(t *testing.T) {
	q := Queue{Confirm: []QueuePending{}, Review: []QueueReview{}, Retro: []retro.Item{}}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"confirm":[]`, `"review":[]`, `"retro":[]`} {
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
