// Package arch 는 코드가 아니라 **구조**를 검사한다.
//
// 스펙 §4.1 의 규칙 — "어댑터는 core 만 부른다. core 는 어댑터를 모른다" — 은 감사에서
// 나온 두 결함(*쓰기 경로가 둘로 갈라져 스키마 검증이 한쪽에만 있다*, *frontmatter
// 방출기가 두 벌*)을 구조적으로 불가능하게 만들려는 것이다. 그런데 지금까지 그 규칙은
// 주석으로만 있었다. 주석은 강제하지 않는다.
//
// 어댑터가 하나뿐일 때는 어길 방법이 없어서 문제가 드러나지 않았다. MCP 어댑터가
// 생기면서 처음으로 어길 수 있게 됐다 — 예컨대 mcp 가 cli 의 warnSkipped 를 쓰고 싶어
// export 하는 순간, 두 어댑터가 한 덩어리가 되고 규칙은 조용히 사라진다.
package arch

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const mod = "github.com/xian0310567/casebook"

// deps 는 pkg 가 (이행적으로) 의존하는 이 모듈 안의 패키지를 준다.
func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		// 경계를 어기는 방식 중 하나가 import 순환이고, 그때 go list 는 종료 코드만
		// 남기고 이유는 stderr 로 보낸다. 그것까지 실어야 "왜 실패했는지 모르겠는
		// 아키텍처 테스트" 가 되지 않는다.
		var detail string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			detail = "\n" + strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("go list -deps %s: %v%s", pkg, err, detail)
	}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, mod+"/") {
			got = append(got, strings.TrimPrefix(line, mod+"/"))
		}
	}
	return got
}

// core 는 어댑터를 모른다. 알게 되는 순간 "core 가 유일한 API" 라는 전제가 깨지고,
// 어댑터가 core 의 동작을 좌우할 수 있게 된다.
func TestCoreDoesNotImportAdapters(t *testing.T) {
	for _, d := range deps(t, mod+"/internal/core/...") {
		if strings.HasPrefix(d, "internal/adapter/") {
			t.Errorf("core 가 어댑터를 import 한다: %s (§4.1 위반)", d)
		}
	}
}

// 어댑터끼리도 서로를 모른다. 공유하고 싶은 것이 생기면 core 로 내리거나 각자 갖는다.
// 어댑터 A 가 어댑터 B 를 부르기 시작하면 B 의 출력 규약이 A 의 요구에 끌려다니고,
// 결국 "CLI 용" 과 "MCP 용" 이 한 함수 안에서 분기하게 된다.
func TestAdaptersDoNotImportEachOther(t *testing.T) {
	adapters := []string{"cli", "mcp"}
	for _, a := range adapters {
		for _, d := range deps(t, mod+"/internal/adapter/"+a) {
			for _, other := range adapters {
				if other != a && d == "internal/adapter/"+other {
					t.Errorf("어댑터 %s 가 어댑터 %s 를 import 한다 (§4.1 위반)", a, other)
				}
			}
		}
	}
}

// core 는 호스트를 모른다. transcript 는 호스트의 파일 형식에 묶인 코드이고,
// daemon 은 배경 프로세스다. core 가 둘 중 하나라도 알게 되면 "core 는 어댑터가
// 부르는 유일한 API" 라는 전제가 깨지고, CLI 로 부르는 경로와 데몬으로 부르는 경로가
// 갈라지기 시작한다.
func TestCoreKnowsNeitherHostNorDaemon(t *testing.T) {
	for _, d := range deps(t, mod+"/internal/core/...") {
		if strings.HasPrefix(d, "internal/transcript") || strings.HasPrefix(d, "internal/daemon") {
			t.Errorf("core 가 %s 를 import 한다 (§4.1 위반)", d)
		}
	}
}

// transcript 는 잎이다. 호스트의 파일을 읽어 Turn 으로 바꾸는 것 말고는 아무것도
// 모른다 — 설정도, 볼트도, 데몬도. 이 인터페이스가 `(파일) → []Turn` 하나로 좁아야
// 호스트 하나가 깨져도 다른 호스트에 번지지 않는다 (스펙 §9).
func TestTranscriptIsALeaf(t *testing.T) {
	for _, d := range deps(t, mod+"/internal/transcript/...") {
		if d == "internal/transcript" || strings.HasPrefix(d, "internal/transcript/") {
			continue
		}
		t.Errorf("transcript 가 %s 를 import 한다 — 잎이어야 한다", d)
	}
}

// 데몬은 core 와 transcript 를 쓰되 어댑터는 모른다.
func TestDaemonDoesNotImportAdapters(t *testing.T) {
	for _, d := range deps(t, mod+"/internal/daemon") {
		if strings.HasPrefix(d, "internal/adapter/") {
			t.Errorf("daemon 이 %s 를 import 한다", d)
		}
	}
}

// 어댑터는 core 를 부르라고 있는 것이다. core 를 하나도 안 부르는 어댑터가 있다면
// 로직을 자기가 들고 있다는 뜻이고, 그게 쓰기 경로가 갈라지는 시작점이다.
func TestAdaptersDoCallCore(t *testing.T) {
	for _, a := range []string{"cli", "mcp"} {
		found := false
		for _, d := range deps(t, mod+"/internal/adapter/"+a) {
			if strings.HasPrefix(d, "internal/core/") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("어댑터 %s 가 core 를 전혀 부르지 않는다", a)
		}
	}
}
