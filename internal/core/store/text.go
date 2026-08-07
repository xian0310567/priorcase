package store

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// slugMaxRunes 는 파일명 slug 의 최대 길이다. 바이트가 아니라 rune 이다 —
// 바이트로 자르면 한글 문자 중간에서 잘려 깨진 UTF-8 파일명이 생긴다.
const slugMaxRunes = 80

// NFC 는 문자열을 NFC 로 정규화한다.
// macOS APFS 는 NFD 이름을 그대로 보존해 돌려주고 Linux ext4 는 바이트 정확 매칭이므로,
// 프로세스 경계를 넘어온 파일명(ReadDir·argv·tar·훅 JSON)은 반드시 통과시킨다.
func NFC(s string) string {
	if norm.NFC.IsNormalString(s) {
		return s // 흔한 경우에 할당을 피한다
	}
	return norm.NFC.String(s)
}

// TruncateRunes 는 앞에서부터 n 개 rune 만 남긴다.
// range 로 문자열을 돌면 인덱스가 항상 rune 경계 바이트 오프셋이다.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// slugBad 는 파일명에 쓸 수 없는 문자다. 허용목록이 아니라 거부목록으로 간다 —
// 허용목록은 한글·이모지·확장 문자를 매번 빠뜨린다.
const slugBad = `/\:*?"<>|`

// Slugify 는 임의 문자열을 파일명 조각으로 바꾼다.
func Slugify(s string) string {
	s = NFC(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(slugBad, r), r == ' ', r == '\t', r == '\n':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-.")
	return TruncateRunes(out, slugMaxRunes)
}
