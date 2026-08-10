// Package testutil 은 테스트용 볼트 픽스처를 만든다.
// store 를 import 하지 않는다 — store 의 테스트가 이걸 쓰면 순환이 되기 때문이다.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/xian0310567/priorcase/internal/core/config"
)

// VaultConfig 는 testdata/vault 를 임시 디렉토리로 복사하고 그것을 가리키는 설정을 준다.
// 도메인 경로는 /tmp/proj/<name> 으로 고정해 cwd 기반 테스트가 결정적이게 한다.
func VaultConfig(t *testing.T) *config.Config {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(fixtureSrc(t))); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		Vault: dst,
		// 폴백 도메인 — 실제 설정과 같은 모양으로 둔다. 없으면 어느 paths 에도
		// 안 걸리는 cwd 에서 기록이 막히는데, 그건 픽스처의 의도가 아니다.
		DefaultDomain: "common",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Paths: []string{"/tmp/proj/alpha"}},
			{Prefix: "beta", Folder: "beta", Paths: []string{"/tmp/proj/beta"}},
			{Prefix: "common", Folder: "common"},
		},
	}
}

// VaultConfigFile 은 VaultConfig 와 같은 볼트·설정을 만들되, 그 설정을 TOML
// 파일로도 써서 (설정 파일 경로, 설정) 을 준다. 볼트 경로는 c.Vault 다.
//
// --config 플래그는 구조체가 아니라 "파일"을 받으므로 CLI 테스트는 설정 파일이
// 필요하다. 그래서 예전에는 cli 패키지의 테스트 4개가 각자 TOML 상수와
// write*CmdFixture 함수를 복제해 갖고 있었다(467줄 중 260줄가량). 그 복제가
// 위험한 이유는 줄 수가 아니다 — [naming] 4키가 다섯 자리(여기 구조체 + TOML
// 상수 4개)에 흩어져 있으면 config 층이 키를 하나 더 요구하는 순간 네 파일을
// 다 고쳐야 하고, 한 곳을 빠뜨리면 그 테스트만 조용히 다른 설정으로 돈다.
//
// 파일 본문은 손으로 쓴 TOML 상수가 아니라 같은 config.Config 를 직렬화해서
// 만든다. 정본이 하나뿐이면 어긋날 자리가 없다. 직렬화기는 config 가 읽을 때
// 쓰는 것과 같은 go-toml/v2 이므로 왕복이 보장된다.
func VaultConfigFile(t *testing.T) (cfgPath string, c *config.Config) {
	t.Helper()
	c = VaultConfig(t)
	body, err := toml.Marshal(c)
	if err != nil {
		t.Fatalf("픽스처 설정을 TOML 로 쓸 수 없다: %v", err)
	}
	cfgPath = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, c
}

// fixtureSrc 는 이 파일의 위치에서 저장소 루트를 거슬러 testdata/vault 를 찾는다.
// 상대 경로("../../testdata")를 쓰면 호출하는 패키지의 깊이에 따라 깨진다.
func fixtureSrc(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("호출자 정보를 얻을 수 없다")
	}
	// self = <repo>/internal/testutil/vault.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	return filepath.Join(root, "testdata", "vault")
}
