package daemon

import (
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// ResolveHosts 는 **설정이 켜 둔 호스트만** 준다.
//
// # 왜 함수 하나인가
//
// 호스트를 푸는 자리가 셋이다 — 데몬의 감시 목록, 훑기(sweep), 스캔의 폴백.
// 각자 설정을 보게 하면 하나를 빠뜨렸을 때 **끈 호스트가 그 경로로만 계속
// 읽힌다.** 그건 조용하다: 사람은 껐다고 믿고, 대화는 계속 훑힌다.
//
// 그래서 통로를 하나로 두고 internal/arch 가 다른 자리의 hosts.Resolve 직접
// 호출을 막는다. 이 프로젝트에서 다섯 번 난 사고가 전부 "값은 있는데 조립이
// 안 읽는다" 였다.
//
// # override 가 이긴다
//
// `--transcript-root` 는 "여기를 봐라" 라는 명시적 지시다. 설정이 그 호스트를
// 꺼 뒀어도 사람이 방금 손으로 지목한 것이 이긴다 — 안 그러면 왜 아무것도 안
// 읽히는지 알 방법이 없다.
//
// # 다 끄는 것은 에러가 아니다
//
// 호스트를 전부 끄면 빈 목록을 준다. Claude Code 가 Required 인 것은 "자리를
// 못 찾으면 배선이 틀린 것" 이라는 뜻이지 "끌 수 없다" 는 뜻이 아니다.
// 명시적으로 끈 것과 없어서 못 찾은 것은 다르다.
func ResolveHosts(c *config.Config, override string) ([]hosts.Resolved, error) {
	if override != "" || c == nil {
		return hosts.Resolve(override)
	}
	var out []hosts.Resolved
	for _, h := range hosts.All() {
		if !c.HostOn(h.Name) {
			continue
		}
		root := c.HostRoot(h.Name)
		if root == "" {
			r, err := h.DefaultRoot()
			if err != nil {
				// 자리를 못 찾는 호스트는 조용히 빠진다 — 그 사람은 그 도구를
				// 안 쓴다. 필수 호스트가 빠지면 배선이 틀린 것이라 알린다.
				if h.Required {
					return nil, err
				}
				continue
			}
			root = r
		}
		out = append(out, hosts.Resolved{Host: h, Root: root})
	}
	return out, nil
}
