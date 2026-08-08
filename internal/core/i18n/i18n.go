// Package i18n 은 **볼트에 남거나 에이전트 컨텍스트로 들어가는** 문자열의 언어를 고른다.
//
// CLI 진단 출력은 대상이 아니다. 그건 사람이 터미널에서 한 번 보고 마는 것이고,
// 볼트 산출물은 남아서 회수되고 다른 사람이 읽는다 — 그 차이가 이 패키지의 범위다.
//
// **키·카탈로그를 쓰지 않는다.** 대상이 여덟 문자열뿐이라 키를 도입하면 간접층이
// 내용보다 커진다. 호출부가 두 언어를 나란히 넘기면 한쪽만 고치다 어긋나는 일도 없다.
package i18n

// Lang 은 볼트 산출물의 언어다.
type Lang string

const (
	KO Lang = "ko"
	EN Lang = "en"
)

// Of 는 설정값을 Lang 으로 바꾼다. 모르는 값이나 빈 값은 한국어다 —
// 이 프로젝트의 기존 볼트가 전부 한국어라 그쪽이 안전한 기본값이다.
func Of(s string) Lang {
	if Lang(s) == EN {
		return EN
	}
	return KO
}

// T 는 언어에 맞는 문자열을 고른다.
func (l Lang) T(ko, en string) string {
	if l == EN {
		return en
	}
	return ko
}
