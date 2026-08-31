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
// 그러면 `prior --version` 이 말하는 것과 실제로 도는 것이 달라진다. 정확히 고정한다.
func TestLauncherPinsPlatformPackagesExactly(t *testing.T) {
	root := repoRoot(t)
	raw := readFile(t, filepath.Join(root, "npm", "priorcase", "package.json"))
	var p struct {
		Bin                  map[string]string `json:"bin"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		Files                []string          `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	// 6개다: darwin·linux·win32 × arm64·x64. 윈도우는 2026-08-31 에 붙였다 —
	// 사내에 윈도우 기획자가 있고 그 사람도 Claude Code 로 작업하고 기록한다.
	if len(p.OptionalDependencies) != 6 {
		t.Errorf("플랫폼 패키지 %d개, want 6 (darwin/linux/win32 × arm64/x64)",
			len(p.OptionalDependencies))
	}

	// ★ **런처가 찾는 이름과 팩 스크립트가 만드는 이름이 같아야 한다.**
	//
	// 어긋나면 npm 이 바이너리를 받아 놓고도 런처가 못 찾아 "no binary for
	// darwin-arm64" 로 죽는다 — 설치는 성공하는데 실행이 안 되는, 사용자가 원인을
	// 짐작할 수 없는 종류다. 스코프를 걷어내며 실제로 세 곳을 같이 고쳐야 했다.
	launcher := readFile(t, filepath.Join(root, "npm", "priorcase", "bin", "prior.js"))
	pack := readFile(t, filepath.Join(root, "scripts", "npm-pack.sh"))
	for _, want := range []string{
		"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64",
		"win32-arm64", "win32-x64",
	} {
		if _, ok := p.OptionalDependencies["priorcase-"+want]; !ok {
			t.Errorf("optionalDependencies 에 priorcase-%s 가 없다", want)
		}
	}
	if !strings.Contains(launcher, "`priorcase-${os}-${arch}`") {
		t.Error("런처가 찾는 패키지 이름이 priorcase-<os>-<arch> 가 아니다")
	}
	// **npmos 다, goos 가 아니다.** node 의 process.platform 은 windows 를 win32 로
	// 부르므로 패키지 이름은 node 가 보는 쪽을 따라야 한다 — 여기가 어긋나면
	// 윈도우 사용자만 "no binary" 로 튕기고 다른 플랫폼에서는 안 보인다.
	if !strings.Contains(pack, `"name": "priorcase-${npmos}-${npmarch}"`) {
		t.Error("팩 스크립트가 만드는 이름이 priorcase-<npmos>-<arch> 가 아니다")
	}
	// 스코프를 쓰려면 npm 조직이 있어야 한다. 조직 이름은 미리 확인할 API 가 없어
	// 제출해 봐야만 알 수 있으므로(옛 이름 casebook 이 그렇게 막혔다), 스코프가
	// 조용히 되살아나면 그날 게시가 통째로 멎는다.
	if strings.Contains(launcher, "@priorcase/") || strings.Contains(pack, "@priorcase/") {
		t.Error("@priorcase 스코프가 되살아났다 — 무스코프로 가기로 했고, 조직은 확보한 적이 없다")
	}
	for name, ver := range p.OptionalDependencies {
		if strings.ContainsAny(ver, "^~><*") {
			t.Errorf("%s 를 %q 로 느슨하게 걸었다 — 런처와 바이너리의 판이 갈린다", name, ver)
		}
	}
	if p.Bin["prior"] == "" {
		t.Error("bin.prior 가 없다 — npx priorcase 가 안 된다")
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

// 플랫폼 패키지는 `os`·`cpu` 를 적어야 한다. 없으면 npm 이 **여섯 개를 전부 받는다** —
// 사용자가 디스크를 6배 쓰고, 다른 플랫폼 바이너리가 섞인다.
func TestPackScriptDeclaresOsAndCpu(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "scripts", "npm-pack.sh"))
	for _, want := range []string{`"os": ["${npmos}"]`, `"cpu": ["${npmarch}"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("npm-pack.sh 에 %s 가 없다 — npm 이 모든 플랫폼 바이너리를 받는다", want)
		}
	}
	// amd64(Go) 와 x64(npm) 는 이름이 다르다. 잘못 매핑하면 아무도 못 받는다.
	// amd64(Go)→x64(npm), windows(Go)→win32(npm). 잘못 매핑하면 아무도 못 받는다.
	for _, want := range []string{"darwin  amd64 x64   darwin", "linux   arm64 arm64 linux",
		"windows amd64 x64   win32", "windows arm64 arm64 win32"} {
		if !strings.Contains(body, want) {
			t.Errorf("npm-pack.sh 의 대상 표에 %q 가 없다 — 그 플랫폼은 배포되지 않는다", want)
		}
	}
}

// 런처는 자기 플랫폼 바이너리가 없을 때 **조용히 넘어가면 안 된다.**
func TestLauncherFailsLoudlyWithoutBinary(t *testing.T) {
	body := readFile(t, filepath.Join(repoRoot(t), "npm", "priorcase", "bin", "prior.js"))
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
