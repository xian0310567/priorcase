// Package hosts 는 어떤 호스트의 대화 기록을 읽을 수 있는지 한자리에 모은다.
//
// **여기가 유일한 목록이다.** 호스트를 늘릴 때 고쳐야 하는 곳이 하나면, 파서를
// 만들어 놓고 배선을 잊는 일이 안 생긴다 — 그건 조용히 실패한다. 파서는 통과하는
// 테스트를 갖고 있는데 아무도 그것을 부르지 않는 상태가 되기 때문이다.
//
// 레지스트리를 transcript 패키지 안에 못 두는 이유는 순환이다: 파서들이
// transcript 를 임포트하므로 transcript 가 파서를 임포트할 수 없다.
package hosts

import (
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
	"github.com/xian0310567/priorcase/internal/transcript/codex"
)

// Host 는 대화 기록을 가진 도구 하나다.
type Host struct {
	// ID 는 **기계가 쓰는 안정된 이름**이다. `hook.Host` 값과 같은 문자열을 쓴다
	// ("claude-code" · "codex") — 훅이 자기 호스트를 그 이름으로 알고 있고, 판별기
	// 종류를 고를 때도 그 이름으로 갈라야 두 자리가 어긋나지 않는다.
	//
	// Name 으로 갈라도 되지만 그건 사람에게 보이는 문자열이라 언젠가 바뀐다.
	// 바뀌면 판별기 선택이 조용히 기본값으로 떨어진다 — 그 종류의 고장을 이
	// 프로젝트가 계속 경계해 왔다.
	ID string
	// Name 은 사람에게 보이는 이름이다. 진단과 로그에 쓴다.
	Name string
	// DefaultRoot 는 이 호스트가 기록을 쌓는 자리다.
	DefaultRoot func() (string, error)
	// List 는 root 아래의 기록 파일을 준다. 못 읽은 디렉토리 수도 같이 준다 —
	// 조용히 줄어든 목록은 "결정이 없었다" 와 구별되지 않는다.
	List func(root string) (paths []string, unreadable int, err error)
	// Parse 는 파일 하나를 읽어 발화를 뽑는다. 계약은 transcript 패키지 문서 참고.
	Parse func(r io.Reader) ([]transcript.Turn, transcript.Meta, int64, int, error)

	// Required 는 이 호스트가 없을 때 알려야 하는가다.
	//
	// Claude Code 는 이 도구의 전제라서 기록 자리가 없으면 배선이 틀린 것이고,
	// 알려야 한다. Codex 는 안 쓰는 사람이 훨씬 많아서, 없다고 매번 말하면
	// **진짜 문제를 가린다** — 소음이 된 경고는 읽히지 않는다.
	Required bool
}

// All 은 지원하는 호스트를 준다.
//
// 순서가 곧 우선순위다. 경로가 어느 호스트에 속하는지 볼 때 앞에서부터 본다.
func All() []Host {
	return []Host{
		{
			ID:          "claude-code",
			Name:        "Claude Code",
			DefaultRoot: claudecode.DefaultRoot,
			List:        claudecode.List,
			Parse:       claudecode.Parse,
			Required:    true,
		},
		{
			ID:          "codex",
			Name:        "Codex CLI",
			DefaultRoot: codex.DefaultRoot,
			List:        codex.List,
			Parse:       codex.Parse,
		},
	}
}

// ClaudeCode 는 Claude Code 호스트 하나만 쓰겠다는 뜻이다.
//
// **훅은 어느 호스트가 자기를 불렀는지 이미 안다.** Claude Code 의 훅이므로
// Claude Code 다 — 경로를 보고 추측할 이유가 없고, 추측하면 호스트가 기록 자리를
// 옮기는 순간 훅이 자기 대화를 못 읽는다.
//
// Root 를 비워 둔다. For 가 루트로 고르지 못하게 하는 대신, 호출자가 목록을
// 하나로 좁혀서 "이걸로 읽어라" 를 말한다.
func ClaudeCode() []Resolved {
	for _, h := range All() {
		if h.Required {
			return []Resolved{{Host: h}}
		}
	}
	return nil
}

// Resolved 는 호스트와 실제로 찾은 루트를 묶은 것이다.
type Resolved struct {
	Host Host
	Root string
}

// Resolve 는 각 호스트의 루트를 찾는다.
//
// **override 가 있으면 그것 하나만 본다.** `prior daemon --transcript-root <경로>`
// 는 "여기를 봐라" 이지 "여기도 봐라" 가 아니다. 나머지 호스트가 기본 자리를 계속
// 보면, 격리된 자리를 지정한 사람이 자기도 모르게 홈 디렉토리 전체를 훑게 된다 —
// 테스트가 실제 ~/.codex 의 세션 1,729개를 훑다가 멈추면서 드러났다. 사용자에게는
// 그것이 "지정한 곳만 볼 줄 알았는데 남의 대화까지 읽혔다" 가 된다.
//
// 루트를 못 찾는 호스트는 조용히 빠진다. Required 인 호스트가 빠지면 에러다.
func Resolve(override string) ([]Resolved, error) {
	if override != "" {
		rs := ClaudeCode()
		if len(rs) == 0 {
			return nil, errNoRequiredHost
		}
		rs[0].Root = override
		return rs, nil
	}
	var out []Resolved
	for _, h := range All() {
		r, err := h.DefaultRoot()
		if err != nil {
			if h.Required {
				return nil, err
			}
			continue
		}
		out = append(out, Resolved{Host: h, Root: r})
	}
	return out, nil
}

var errNoRequiredHost = errors.New("필수 호스트가 목록에 없다")

// For 는 경로가 어느 호스트의 것인지 고른다.
//
// **못 고르면 nil 이다. 기본값으로 떨어지지 않는다.** 엉뚱한 파서로 읽으면 발화가
// 0개로 나오는데, 그건 "그 세션에 결정이 없었다" 와 구별되지 않는다 — 조용히 틀린다.
func For(path string, rs []Resolved) *Resolved {
	// **루트 없는 항목 하나뿐이면 그것이다.** 호출자가 이미 호스트를 아는 경우다
	// (훅). 루트가 비어 있다는 것이 "경로로 고르지 마라" 는 신호다.
	//
	// 루트가 **있으면** 개수와 무관하게 경로로 맞춘다. 처음에 `len(rs)==1` 만 보고
	// 넘겼더니, 루트를 지정한 목록 하나짜리도 무조건 통과해서 남의 자리 파일을
	// 그 파서로 읽었다 — 변형 테스트가 잡았다.
	if len(rs) == 1 && rs[0].Root == "" {
		return &rs[0]
	}
	best := -1
	bestLen := 0
	for i, r := range rs {
		if r.Root == "" || !under(path, r.Root) {
			continue
		}
		// 루트가 겹치면 **더 깊은 쪽**이 이긴다. 한쪽이 다른 쪽 안에 있을 때
		// 얕은 것이 먼저 잡으면 그 호스트의 파일을 남의 파서로 읽는다.
		if n := len(r.Root); n > bestLen {
			best, bestLen = i, n
		}
	}
	if best < 0 {
		return nil
	}
	return &rs[best]
}

// under 는 path 가 root 아래인지 본다. 경로 조각 경계로만 맞춘다 —
// 문자열 접두어로 보면 `/a/bc` 가 `/a/b` 아래로 잡힌다.
func under(path, root string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
