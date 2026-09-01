package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// ★★★ **빈 목록은 `[]` 여야 한다. `null` 이면 앱이 순회하다 터진다.**
//
// 2026-09-01 사고: 앱에서 볼트 `회사` 를 만들었더니 **볼트 화면이 통째로 사라졌다.**
// 새 볼트는 아직 아무 프로젝트도 안 쓰므로 `byVault["회사"]` 가 없는 키였고,
// Go 의 nil 슬라이스는 JSON `null` 로 나간다. 앱이 `v.domains.length` 를 읽다
// TypeError 를 냈고, 그 예외가 렌더를 끊어 뒤쪽(새 볼트 폼·프로젝트 목록)이
// 전부 안 그려졌다. 새로고침해도 같은 자리에서 또 터지니 영구적으로 보였다.
//
// **이건 두 번째다.** 같은 고장이 `show --json` 의 summary_history 에서 먼저 났고
// (그때는 검은 화면), 그 교훈을 이 명령에 안 옮겼다.
//
// **볼트를 실제로 만드는 경로를 지나간다.** 픽스처에 손으로 빈 볼트를 적으면
// 진짜 경로에서만 나는 이 고장을 못 잡는다.
func TestNewVaultDomainsAreEmptyListNotNull(t *testing.T) {
	cfg := settingsFixtureAt(t)
	if _, errb, err := runSettingsCmd(t, "vault", "add", "회사", "--config", cfg); err != nil {
		t.Fatalf("볼트를 못 만들었다: %v (stderr=%s)", err, errb)
	}
	out, errb, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}

	// **문자열로 본다.** 구조체로 되읽으면 null 과 [] 가 똑같이 빈 슬라이스가
	// 되어 이 시험이 아무것도 안 잡는다 — 앱이 보는 것은 바이트다.
	if strings.Contains(strings.ReplaceAll(out, " ", ""), `"domains":null`) {
		t.Errorf("domains 가 null 로 나갔다 — 앱이 순회하다 터진다\n%s", out)
	}

	var s struct {
		Vaults []struct {
			Name    string          `json:"name"`
			Domains json.RawMessage `json:"domains"`
		} `json:"vaults"`
	}
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatalf("JSON 이 아니다: %v\n%s", jerr, out)
	}
	found := false
	for _, v := range s.Vaults {
		if v.Name != "회사" {
			continue
		}
		found = true
		if string(v.Domains) != "[]" {
			t.Errorf("새 볼트의 domains 가 %s — [] 여야 한다", v.Domains)
		}
	}
	if !found {
		t.Fatalf("만든 볼트가 출력에 없다:\n%s", out)
	}
}
