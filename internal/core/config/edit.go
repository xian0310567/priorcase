package config

import (
	"fmt"
	"reflect"
	"strings"
)

// 이 파일은 **설정 파일을 사람이 쓴 그대로 두고** 값 하나만 고친다.
//
// # 왜 줄 단위 수술인가
//
// 파싱해서 구조체로 만든 뒤 toml.Marshal 로 다시 쓰면 한 줄이면 끝난다. 그런데
// 실사용 설정 파일에는 **손으로 쓴 주석이 30줄 넘게** 있다 — 왜 NOI 를 제외했는지,
// 2026-08-09 에 무엇이 거짓으로 판명됐는지, 어느 값이 옛 셸 구현에서 온 것인지.
// Marshal 은 그것을 전부 지우고, 지워진 것은 되살릴 수 없다.
//
// 그래서 원본 줄을 그대로 두고 필요한 줄만 넣고 뺀다.
//
// # 줄 수술의 위험과 그물
//
// 줄을 잘못 건드리면 **파싱은 되는데 뜻이 달라질 수 있다.** 대표적인 사고:
//
//	vault = "~/볼트"      →  [[vault]]        ← 이 자리에서 바꾸면
//	                          name = "personal"
//	exclude = ["~/NOI"]       path = "~/볼트"
//	                          exclude = [...]  ← exclude 가 볼트 안으로 빨려든다
//
// TOML 은 테이블 머리 뒤의 최상위 키를 그 테이블 것으로 읽는다. 그래서 이런
// 변환은 **제자리에서 하지 않고 파일 끝으로 옮긴다.**
//
// 그리고 그것을 사람이 지키게 두지 않는다. edit 는 고치기 전후를 각각 파싱해
// **의도한 변경 딱 하나만 일어났는지** 비교하고, 다르면 결과를 버린다. 위 사고는
// exclude 가 옮겨간 것이 비교에서 걸리므로 파일에 닿지 못한다.

// edit 는 줄 수술을 하고, 그 결과가 의도한 변경 하나와 정확히 일치하는지 본다.
//
// want 는 "이렇게 되어야 한다" 를 원본에서 파싱한 설정에 적용하는 함수다.
// 수술이 그 밖의 것을 건드렸으면 여기서 걸리고 에러가 된다 — 부분적으로 망가진
// 설정을 쓰느니 아무것도 안 쓰는 편이 낫다.
func edit(src []byte, mutate func([]string) ([]string, error), want func(*Config)) ([]byte, error) {
	// **두 번 파싱한다.** 한 번 파싱해서 복사하면 슬라이스가 원본과 바닥을
	// 공유해서, want 가 바꾼 것이 비교 대상까지 같이 바꿔 놓는다 — 그러면
	// 무엇을 해도 통과하는 그물이 된다.
	before, err := parseBytes(src)
	if err != nil {
		return nil, fmt.Errorf("지금 설정을 읽을 수 없어 고칠 수 없다: %w", err)
	}
	expect, err := parseBytes(src)
	if err != nil {
		return nil, err
	}
	want(expect)

	lines, err := mutate(splitLines(src))
	if err != nil {
		return nil, err
	}
	// **끝 개행을 지킨다.** 블록을 파일 끝에 붙이면 마지막 줄에 개행이 없어지고,
	// 그 다음 편집이 그 줄에 이어 붙어 두 키가 한 줄이 된다 — 그때는 파싱이
	// 깨지므로 그물에 걸리지만, 사람이 손으로 열었을 때도 어색하다.
	out := []byte(strings.Join(lines, "\n"))
	if n := len(out); n > 0 && out[n-1] != '\n' {
		out = append(out, '\n')
	}

	after, err := parseBytes(out)
	if err != nil {
		return nil, fmt.Errorf("고친 설정이 다시 읽히지 않는다 — 쓰지 않았다: %w", err)
	}
	if !reflect.DeepEqual(expect, after) {
		return nil, fmt.Errorf("고친 결과가 의도와 다르다 — 쓰지 않았다 (%s)", diffHint(before, expect, after))
	}
	return out, nil
}

// diffHint 는 무엇이 어긋났는지 한 줄로 말한다. 사람이 이 에러를 보면 버그
// 신고를 해야 하므로, "다르다" 만으로는 부족하다.
func diffHint(before, expect, after *Config) string {
	switch {
	case !reflect.DeepEqual(expect.Vaults, after.Vaults):
		return fmt.Sprintf("볼트 %d개를 기대했는데 %d개다", len(expect.Vaults), len(after.Vaults))
	case !reflect.DeepEqual(expect.Domain, after.Domain):
		return fmt.Sprintf("도메인 %d개를 기대했는데 %d개다", len(expect.Domain), len(after.Domain))
	case !reflect.DeepEqual(expect.Host, after.Host):
		return fmt.Sprintf("호스트 %d개를 기대했는데 %d개다", len(expect.Host), len(after.Host))
	case !reflect.DeepEqual(before.Exclude, after.Exclude):
		return "exclude 가 딸려 갔다"
	}
	return "그 밖의 값이 바뀌었다"
}

func splitLines(src []byte) []string { return strings.Split(string(src), "\n") }

// tomlString 은 값을 TOML 기본 문자열로 만든다.
//
// strconv.Quote 를 쓰지 않는 이유: 그것은 한글을 \uXXXX 로 escape 해서, 사람이
// 열었을 때 자기가 적은 도메인 이름을 못 알아본다. 설정 파일은 사람이 읽는다.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// isHeader 는 이 줄이 테이블 머리인지 본다 ([x] 또는 [[x]]).
func isHeader(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")
}

// headerIs 는 이 줄이 정확히 그 테이블 머리인지 본다. 공백은 무시한다.
func headerIs(line, name string) bool {
	t := strings.TrimSpace(line)
	return t == "[["+name+"]]" || t == "["+name+"]"
}

// keyOf 는 `key = value` 줄에서 키 이름을 준다. 아니면 빈 문자열.
func keyOf(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || isHeader(t) {
		return ""
	}
	i := strings.IndexByte(t, '=')
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(t[:i])
}

// stringValueOf 는 `key = "value"` 줄에서 값을 준다. 따옴표가 아니면 빈 문자열.
func stringValueOf(line string) string {
	t := strings.TrimSpace(line)
	i := strings.IndexByte(t, '=')
	if i < 0 {
		return ""
	}
	v := strings.TrimSpace(t[i+1:])
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return ""
}

// blockOf 는 name 테이블 배열의 i번째 블록 범위를 [시작, 끝) 으로 준다.
// match 가 참인 첫 블록을 고른다. 없으면 (-1, -1).
func blockOf(lines []string, name string, match func(body []string) bool) (int, int) {
	for i := 0; i < len(lines); i++ {
		if !headerIs(lines[i], name) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if isHeader(lines[j]) {
				end = j
				break
			}
		}
		if match == nil || match(lines[i+1:end]) {
			return i, end
		}
		i = end - 1
	}
	return -1, -1
}

// trimTrailingBlanks 는 파일 끝의 빈 줄을 걷어낸다. 블록을 덧붙이기 전에
// 부르면 빈 줄이 계속 쌓이지 않는다.
func trimTrailingBlanks(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// AddVault 는 볼트를 하나 더한다.
//
// 설정이 아직 `vault = "..."` 한 줄짜리면 **[[vault]] 두 벌로 바꾼다.** 그 변환은
// 제자리에서 하면 뒤따르는 최상위 키를 삼키므로 (파일 머리 주석 참고) 그 줄을
// 지우고 파일 끝에 두 블록을 쓴다.
func AddVault(src []byte, name, path string) ([]byte, error) {
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if name == "" {
		return nil, fmt.Errorf("볼트 이름이 비었다")
	}
	if path == "" {
		return nil, fmt.Errorf("볼트 경로가 비었다")
	}
	cur, err := parseBytes(src)
	if err != nil {
		return nil, fmt.Errorf("지금 설정을 읽을 수 없어 고칠 수 없다: %w", err)
	}
	for _, v := range cur.Vaults {
		if v.Name == name {
			return nil, fmt.Errorf("볼트 %q 는 이미 있다 (%s)", name, v.Path)
		}
	}

	block := func(n, p string) []string {
		return []string{"", "[[vault]]", "name = " + tomlString(n), "path = " + tomlString(p)}
	}

	return edit(src, func(lines []string) ([]string, error) {
		// 스칼라 형태면 그 줄을 걷어내고 끝에서 두 벌로 되살린다.
		var legacy string
		for i, l := range lines {
			if keyOf(l) != "vault" {
				continue
			}
			// 테이블 안의 vault 키(도메인의 vault)는 건드리면 안 된다.
			if inTable(lines, i) {
				continue
			}
			legacy = stringValueOf(l)
			lines = append(lines[:i:i], lines[i+1:]...)
			break
		}
		lines = trimTrailingBlanks(lines)
		if legacy != "" {
			lines = append(lines, block(DefaultVaultName, legacy)...)
		}
		return append(lines, block(name, path)...), nil
	}, func(c *Config) {
		c.Vaults = append(c.Vaults, Vault{Name: name, Path: path})
	})
}

// inTable 은 i번째 줄이 어떤 테이블 머리 아래에 있는지 본다.
func inTable(lines []string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		if isHeader(lines[j]) {
			return true
		}
	}
	return false
}

// BindDomain 은 도메인 하나가 쓸 볼트를 정한다.
//
// 도메인 블록 **안에** 줄을 넣으므로 파일 끝으로 옮기지 않는다 — 이미 테이블
// 아래라 뒤따르는 키를 삼킬 일이 없다.
func BindDomain(src []byte, prefix, vault string) ([]byte, error) {
	prefix, vault = strings.TrimSpace(prefix), strings.TrimSpace(vault)
	cur, err := parseBytes(src)
	if err != nil {
		return nil, fmt.Errorf("지금 설정을 읽을 수 없어 고칠 수 없다: %w", err)
	}
	idx := -1
	for i, d := range cur.Domain {
		if d.Prefix == prefix {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("도메인 %q 가 설정에 없다", prefix)
	}
	// **없는 볼트로 엮으면 그 도메인의 기록이 통째로 갈 곳을 잃는다.** 빈 값은
	// "기본 볼트" 라는 뜻이므로 허용한다.
	if vault != "" {
		known := false
		for _, v := range cur.Vaults {
			if v.Name == vault {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("볼트 %q 가 설정에 없다 — 먼저 만들어야 한다", vault)
		}
	}

	return edit(src, func(lines []string) ([]string, error) {
		s, e := blockOf(lines, "domain", func(body []string) bool {
			for _, l := range body {
				if keyOf(l) == "prefix" && stringValueOf(l) == prefix {
					return true
				}
			}
			return false
		})
		if s < 0 {
			return nil, fmt.Errorf("도메인 %q 블록을 설정 파일에서 찾지 못했다", prefix)
		}
		line := "vault  = " + tomlString(vault)
		for i := s + 1; i < e; i++ {
			if keyOf(lines[i]) == "vault" {
				if vault == "" {
					return append(lines[:i:i], lines[i+1:]...), nil
				}
				lines[i] = line
				return lines, nil
			}
		}
		if vault == "" {
			return lines, nil // 이미 없다
		}
		// prefix 줄 바로 뒤에 넣는다 — 사람이 블록을 훑을 때 가장 먼저 보는 자리다.
		at := s + 1
		for i := s + 1; i < e; i++ {
			if keyOf(lines[i]) == "prefix" {
				at = i + 1
				break
			}
		}
		out := append([]string{}, lines[:at]...)
		out = append(out, line)
		return append(out, lines[at:]...), nil
	}, func(c *Config) {
		c.Domain[idx].Vault = vault
	})
}

// SetHost 는 호스트 하나를 켜거나 끈다.
//
// 없으면 파일 끝에 블록을 만든다. `[[host]]` 는 테이블 배열이라 끝에 붙여도
// 뒤에 아무것도 없어 삼킬 것이 없다.
func SetHost(src []byte, name string, enabled bool, root string) ([]byte, error) {
	name, root = strings.TrimSpace(name), strings.TrimSpace(root)
	if name == "" {
		return nil, fmt.Errorf("호스트 이름이 비었다")
	}
	cur, err := parseBytes(src)
	if err != nil {
		return nil, fmt.Errorf("지금 설정을 읽을 수 없어 고칠 수 없다: %w", err)
	}
	idx := -1
	for i, h := range cur.Host {
		if h.Name == name {
			idx = i
			break
		}
	}

	return edit(src, func(lines []string) ([]string, error) {
		s, e := blockOf(lines, "host", func(body []string) bool {
			for _, l := range body {
				if keyOf(l) == "name" && stringValueOf(l) == name {
					return true
				}
			}
			return false
		})
		set := func(lines []string, s, e int, key, val string) []string {
			for i := s + 1; i < e; i++ {
				if keyOf(lines[i]) == key {
					if val == "" {
						return append(lines[:i:i], lines[i+1:]...)
					}
					lines[i] = key + " = " + val
					return lines
				}
			}
			if val == "" {
				return lines
			}
			out := append([]string{}, lines[:e]...)
			out = append(out, key+" = "+val)
			return append(out, lines[e:]...)
		}
		if s < 0 {
			lines = trimTrailingBlanks(lines)
			lines = append(lines, "", "[[host]]", "name = "+tomlString(name),
				"enabled = "+fmt.Sprint(enabled))
			if root != "" {
				lines = append(lines, "root = "+tomlString(root))
			}
			return lines, nil
		}
		lines = set(lines, s, e, "enabled", fmt.Sprint(enabled))
		// enabled 를 넣거나 빼면서 블록 길이가 바뀌었으므로 다시 찾는다.
		s2, e2 := blockOf(lines, "host", func(body []string) bool {
			for _, l := range body {
				if keyOf(l) == "name" && stringValueOf(l) == name {
					return true
				}
			}
			return false
		})
		return set(lines, s2, e2, "root", func() string {
			if root == "" {
				return ""
			}
			return tomlString(root)
		}()), nil
	}, func(c *Config) {
		on := enabled
		h := Host{Name: name, Enabled: &on, Root: root}
		if idx >= 0 {
			c.Host[idx] = h
			return
		}
		c.Host = append(c.Host, h)
	})
}
