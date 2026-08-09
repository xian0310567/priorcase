package arch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ★★ 런처와 플랫폼 패키지의 판이 갈리면 **진단이 가장 어려운 종류의 고장**이 된다.
//
// 런처가 `^1.2.0` 으로 느슨하게 걸면 npm 이 다른 판의 바이너리를 물어 올 수 있고,
// 그러면 `cb --version` 이 말하는 것과 실제로 도는 것이 달라진다. 정확히 고정한다.
func TestLauncherPinsPlatformPackagesExactly(t *testing.T) {
	root := repoRoot(t)
	raw := readFile(t, filepath.Join(root, "npm", "casebook", "package.json"))
	var p struct {
		Bin                  map[string]string `json:"bin"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		Files                []string          `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.OptionalDependencies) != 4 {
		t.Errorf("플랫폼 패키지 %d개, want 4 (darwin/linux × arm64/x64)", len(p.OptionalDependencies))
	}
	for name, ver := range p.OptionalDependencies {
		if strings.ContainsAny(ver, "^~><*") {
			t.Errorf("%s 를 %q 로 느슨하게 걸었다 — 런처와 바이너리의 판이 갈린다", name, ver)
		}
	}
	if p.Bin["cb"] == "" {
		t.Error("bin.cb 가 없다 — npx casebook 이 안 된다")
	}
	// 라이선스 파일이 패키지에 들어가야 한다. 독점 소프트웨어다.
	for _, want := range []string{"LICENSE", "THIRD-PARTY-NOTICES.md"} {
		found := false
		for _, f := range p.Files {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Errorf("files 에 %s 가 없다 — 라이선스 위반이다", want)
		}
	}
}

// 플랫폼 패키지는 `os`·`cpu` 를 적어야 한다. 없으면 npm 이 **네 개를 전부 받는다** —
// 사용자가 디스크를 4배 쓰고, 다른 플랫폼 바이너리가 섞인다.
func TestPackScriptDeclaresOsAndCpu(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "scripts", "npm-pack.sh"))
	for _, want := range []string{`"os": ["${goos}"]`, `"cpu": ["${npmarch}"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("npm-pack.sh 에 %s 가 없다 — npm 이 모든 플랫폼 바이너리를 받는다", want)
		}
	}
	// amd64(Go) 와 x64(npm) 는 이름이 다르다. 잘못 매핑하면 아무도 못 받는다.
	if !strings.Contains(body, "darwin amd64 x64") || !strings.Contains(body, "linux arm64 arm64") {
		t.Error("goreleaser 의 goarch 와 npm 의 process.arch 매핑이 빠졌다")
	}
}

// 런처는 자기 플랫폼 바이너리가 없을 때 **조용히 넘어가면 안 된다.**
func TestLauncherFailsLoudlyWithoutBinary(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "npm", "casebook", "bin", "cb.js"))
	for _, want := range []string{"process.exit(1)", "--include=optional"} {
		if !strings.Contains(body, want) {
			t.Errorf("런처에 %q 가 없다 — 설치 실패가 조용해진다", want)
		}
	}
	// 종료 코드를 그대로 넘겨야 한다. 훅은 종료 코드가 규약이다.
	if !strings.Contains(body, "r.status === null ? 1 : r.status") {
		t.Error("종료 코드를 그대로 안 넘긴다 — 훅 규약이 깨진다")
	}
	if !strings.Contains(body, "r.signal") {
		t.Error("신호를 안 넘긴다 — Ctrl-C 가 상위 셸에 안 전해진다")
	}
}

var _ = os.Getenv
