package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// 이 파일은 **판이 갈렸다는 사실을 깨지기 전에 보이게** 한다.
//
// # 왜 필요한가
//
// 2026-08-21 에 집이 `Supersedes` 를 다중값으로 올리고 회사는 옛 판이었다. 아무도
// 그 사실을 몰랐고, 노트가 안 읽히고 나서야 드러났다 — 그때는 이미 사람이 손댈
// 준비가 된 뒤였고, 실제로 옛 모양으로 되돌려 다중값을 강등시켰다.
//
// # 왜 schema.Current 가 아닌가
//
// 판 번호는 **사람이 올려야 한다.** 이번 사고가 정확히 안 올려서 났다 — `LinkList`
// 변경이 "기존 노트의 바이트가 안 바뀌니 호환된다" 는 판단으로 판을 그대로 뒀는데,
// 그건 읽기 호환만 본 것이었다. 같은 실수를 반복하지 않으려면 **아무도 기억할
// 필요가 없는 신호**여야 한다.
//
// Go 가 그걸 공짜로 준다: `vcs.revision` 과 `vcs.time` 이 빌드에 자동으로 박힌다.
// `vcs.time` 은 빌드 시각이 아니라 **커밋 시각**이라 더 낫다 — 언제 컴파일했는지가
// 아니라 코드가 얼마나 새것인지를 말한다.
//
// # 왜 머신마다 파일 하나인가
//
// 한 파일을 공유하면 두 머신이 같은 줄을 고쳐 동기화할 때마다 충돌한다. 색인을
// 추적하지 않기로 한 것과 같은 이유다(그쪽 주석 참고).

// stampDirName 은 볼트 안에서 판 도장이 사는 곳이다.
//
// 점으로 시작해 옵시디언 파일 목록에 안 뜬다 — 사람이 읽을 것이 아니다.
// `_meta` 밑에 두는 이유: 볼트 루트가 프로젝트 폴더 자리라 거기 잡동사니를
// 두면 도메인 폴더와 섞인다.
var stampDirName = filepath.Join("_meta", ".priorcase")

// Build 는 이 바이너리가 어느 코드에서 왔는지다.
type Build struct {
	Host     string `json:"host"`
	Revision string `json:"revision"`
	// Committed 는 그 커밋의 시각이다 — **코드의 나이**. 빌드 시각이 아니다.
	Committed time.Time `json:"committed"`
	// Modified 는 커밋 안 된 변경 위에서 빌드했다는 뜻이다. 그러면 Revision 이
	// 실제 코드를 안 가리키므로 비교 결과를 곧이곧대로 믿으면 안 된다.
	Modified bool `json:"modified"`
}

// ThisBuild 는 지금 도는 바이너리의 정보다.
//
// 빌드 정보가 없으면(예: `go run`, 또는 VCS 밖 빌드) Committed 가 제로다.
// 그때는 비교에서 빠진다 — 모르는 것을 안다고 하지 않는다.
func ThisBuild() Build {
	b := Build{Host: hostname()}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision = s.Value
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				b.Committed = t.UTC()
			}
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	return b
}

// RecordBuild 는 이 머신의 판을 볼트에 남긴다.
//
// **바뀐 게 없으면 쓰지 않는다.** 매번 쓰면 동기화할 때마다 이 파일 하나 때문에
// 커밋이 생겨서 "보낼 것 없음" 이 영영 안 나오고, 볼트 원장이 판 도장으로 도배된다.
// 그래서 시각 같은 매번 달라지는 값을 담지 않는다 — 담는 순간 그 문제가 돌아온다.
func RecordBuild(vault string, b Build) error {
	if b.Host == "" {
		return nil // 머신을 못 가리면 남길 것이 없다. 실패로 만들지는 않는다.
	}
	dir := filepath.Join(vault, stampDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	want, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	want = append(want, '\n')
	p := filepath.Join(dir, safeName(b.Host)+".json")
	if cur, err := os.ReadFile(p); err == nil && string(cur) == string(want) {
		return nil
	}
	return os.WriteFile(p, want, 0o644)
}

// NewerBuilds 는 **나보다 새 코드로 도는 다른 머신**을 준다. 오래된 순.
//
// 내 Committed 가 제로면(빌드 정보 없음) 아무것도 안 준다 — 비교 기준이 없는데
// 경고를 내면 그건 늘 뜨는 경고가 되고, 늘 뜨는 경고는 무시를 가르친다.
func NewerBuilds(vault string, self Build) []Build {
	if self.Committed.IsZero() {
		return nil
	}
	dir := filepath.Join(vault, stampDirName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Build
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var b Build
		if err := json.Unmarshal(raw, &b); err != nil {
			continue
		}
		if b.Host == self.Host || b.Committed.IsZero() {
			continue
		}
		if b.Committed.After(self.Committed) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Committed.Before(out[j].Committed) })
	return out
}

// safeName 은 호스트 이름을 파일명으로 쓸 수 있게 다듬는다.
// 호스트 이름에 `/` 가 들어갈 일은 없지만, 볼트에 쓰는 경로라 막아 둔다.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
